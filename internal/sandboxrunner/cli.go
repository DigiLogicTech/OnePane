package sandboxrunner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type CLIEngine struct{}

func NewCLIEngine() *CLIEngine { return &CLIEngine{} }

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remain := b.max - b.Len()
	if remain > 0 {
		if len(p) > remain {
			_, _ = b.Buffer.Write(p[:remain])
		} else {
			_, _ = b.Buffer.Write(p)
		}
	}
	return n, nil
}

func runCLI(ctx context.Context, exe string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	out := &limitedBuffer{max: 1 << 20}
	er := &limitedBuffer{max: 1 << 20}
	cmd.Stdout = out
	cmd.Stderr = er
	err := cmd.Run()
	return strings.TrimSpace(out.String()), strings.TrimSpace(er.String()), err
}

func (e *CLIEngine) Probe(ctx context.Context) (EngineProfile, error) {
	if p, err := exec.LookPath("podman"); err == nil {
		if prof, err := probePodman(ctx, p); err == nil {
			return prof, nil
		}
	}
	if p, err := exec.LookPath("docker"); err == nil {
		if prof, err := probeDocker(ctx, p); err == nil {
			return prof, nil
		}
	}
	return EngineProfile{}, ErrEngineUnavailable
}

func (e *CLIEngine) EnsureNetwork(ctx context.Context, runtimeID string, internal bool) (NetworkState, error) {
	p, err := e.Probe(ctx)
	if err != nil {
		return NetworkState{}, err
	}
	if st, err := e.inspectNetworkWithProfile(ctx, p, runtimeID); err == nil {
		if st.RuntimeID != runtimeID {
			return NetworkState{}, ErrNetworkBackendRequired
		}
		if st.Internal == internal {
			return st, nil
		}
		// A policy transition requires recreating the dedicated project network.
		// The engine will refuse removal while containers are still attached,
		// preserving fail-closed behavior until the runtime is drained.
		if _, stderr, err := runCLI(ctx, p.Executable, "network", "rm", st.Name); err != nil {
			return NetworkState{}, fmt.Errorf("replace project network: %v: %s", err, stderr)
		}
	}
	name := runtimeNetworkName(runtimeID)
	args := []string{"network", "create"}
	if internal {
		args = append(args, "--internal")
	}
	args = append(args, "--label", "io.harness.runtime-id="+runtimeID, name)
	if _, stderr, err := runCLI(ctx, p.Executable, args...); err != nil {
		if !strings.Contains(strings.ToLower(stderr), "already exists") {
			return NetworkState{}, fmt.Errorf("create project network: %v: %s", err, stderr)
		}
	}
	st, err := e.inspectNetworkWithProfile(ctx, p, runtimeID)
	if err != nil {
		return NetworkState{}, err
	}
	if st.Internal != internal || st.RuntimeID != runtimeID {
		return NetworkState{}, ErrNetworkBackendRequired
	}
	return st, nil
}

func (e *CLIEngine) InspectNetwork(ctx context.Context, runtimeID string) (NetworkState, error) {
	p, err := e.Probe(ctx)
	if err != nil {
		return NetworkState{}, err
	}
	return e.inspectNetworkWithProfile(ctx, p, runtimeID)
}

func (e *CLIEngine) inspectNetworkWithProfile(ctx context.Context, p EngineProfile, runtimeID string) (NetworkState, error) {
	name := runtimeNetworkName(runtimeID)
	out, stderr, err := runCLI(ctx, p.Executable, "network", "inspect", name)
	if err != nil {
		return NetworkState{}, fmt.Errorf("inspect network: %v: %s", err, stderr)
	}
	var docs []struct {
		Name     string            `json:"Name"`
		ID       string            `json:"Id"`
		Internal bool              `json:"Internal"`
		Labels   map[string]string `json:"Labels"`
	}
	if err := json.Unmarshal([]byte(out), &docs); err != nil || len(docs) != 1 {
		return NetworkState{}, fmt.Errorf("inspect network response: %w", ErrInvalidInput)
	}
	rid := ""
	if docs[0].Labels != nil {
		rid = docs[0].Labels["io.harness.runtime-id"]
	}
	return NetworkState{Name: docs[0].Name, ID: docs[0].ID, RuntimeID: rid, Internal: docs[0].Internal}, nil
}
func probePodman(ctx context.Context, exe string) (EngineProfile, error) {
	out, _, err := runCLI(ctx, exe, "info", "--format", "json")
	if err != nil {
		return EngineProfile{}, err
	}
	var doc struct {
		Host struct {
			Security struct {
				Rootless bool `json:"rootless"`
			} `json:"security"`
		} `json:"host"`
	}
	if json.Unmarshal([]byte(out), &doc) != nil || !doc.Host.Security.Rootless {
		return EngineProfile{}, ErrRootlessRequired
	}
	return EngineProfile{Kind: "podman", Executable: exe, Rootless: true}, nil
}
func probeDocker(ctx context.Context, exe string) (EngineProfile, error) {
	out, _, err := runCLI(ctx, exe, "info", "--format", "{{json .SecurityOptions}}")
	if err != nil {
		return EngineProfile{}, err
	}
	var opts []string
	if json.Unmarshal([]byte(out), &opts) != nil {
		return EngineProfile{}, ErrRootlessRequired
	}
	for _, v := range opts {
		if strings.Contains(strings.ToLower(v), "rootless") {
			return EngineProfile{Kind: "docker", Executable: exe, Rootless: true}, nil
		}
	}
	return EngineProfile{}, ErrRootlessRequired
}
func (e *CLIEngine) PullImage(ctx context.Context, image string) (ImageState, error) {
	p, err := e.Probe(ctx)
	if err != nil {
		return ImageState{}, err
	}
	if _, stderr, err := runCLI(ctx, p.Executable, "pull", image); err != nil {
		return ImageState{}, fmt.Errorf("pull image: %v: %s", err, stderr)
	}
	return e.InspectImage(ctx, image)
}
func (e *CLIEngine) InspectImage(ctx context.Context, image string) (ImageState, error) {
	p, err := e.Probe(ctx)
	if err != nil {
		return ImageState{}, err
	}
	out, stderr, err := runCLI(ctx, p.Executable, "image", "inspect", image, "--format", "{{json .RepoDigests}}")
	if err != nil {
		return ImageState{}, fmt.Errorf("inspect image: %v: %s", err, stderr)
	}
	var digests []string
	_ = json.Unmarshal([]byte(out), &digests)
	return ImageState{Reference: image, Digests: digests}, nil
}
func (e *CLIEngine) EnsureContainer(ctx context.Context, s ContainerSpec) (ContainerState, error) {
	p, err := e.Probe(ctx)
	if err != nil {
		return ContainerState{}, err
	}
	name := containerName(s.RuntimeID, s.ApplicationID)
	hash := specHash(s)
	out, stderr, inspectErr := runCLI(ctx, p.Executable, "container", "inspect", name, "--format", `{{index .Config.Labels "io.harness.spec-hash"}}|{{.State.Status}}|{{.Id}}`)
	if inspectErr == nil {
		parts := strings.SplitN(out, "|", 3)
		if len(parts) == 3 && parts[0] == hash {
			if parts[1] == "running" {
				return ContainerState{Name: name, ID: parts[2], Status: parts[1], SpecHash: hash}, nil
			}
			if _, stderr, err := runCLI(ctx, p.Executable, "start", name); err != nil {
				return ContainerState{}, fmt.Errorf("start container: %v: %s", err, stderr)
			}
			return ContainerState{Name: name, ID: parts[2], Status: "running", SpecHash: hash}, nil
		}
		if _, stderr, err := runCLI(ctx, p.Executable, "rm", "-f", name); err != nil {
			return ContainerState{}, fmt.Errorf("replace container: %v: %s", err, stderr)
		}
	} else if !containerNotFound(stderr) {
		return ContainerState{}, fmt.Errorf("inspect container: %v: %s", inspectErr, stderr)
	}
	network, err := e.EnsureNetwork(ctx, s.RuntimeID, s.NetworkInternal)
	if err != nil || network.Internal != s.NetworkInternal || network.RuntimeID != s.RuntimeID {
		if err == nil {
			err = ErrNetworkBackendRequired
		}
		return ContainerState{}, err
	}
	args := []string{"run", "-d", "--pull=never", "--name", name, "--label", "io.harness.runtime-id=" + s.RuntimeID, "--label", "io.harness.application-id=" + s.ApplicationID, "--label", "io.harness.spec-hash=" + hash, "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--network=" + network.Name, "--pids-limit", strconv.Itoa(s.Limits.PIDs), "--memory", strconv.Itoa(s.Limits.MemoryMB) + "m", "--cpus", fmt.Sprintf("%.3f", float64(s.Limits.CPUMillis)/1000.0), "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=64m", "--mount", "type=bind,src=" + filepath.Clean(s.WorkspacePath) + ",dst=/workspace,rw"}
	if s.WorkingDir != "" {
		args = append(args, "--workdir", s.WorkingDir)
	}
	for _, port := range s.Ports {
		proto := strings.ToLower(strings.TrimSpace(port.Protocol))
		if proto == "" {
			proto = "tcp"
		}
		args = append(args, "--publish", fmt.Sprintf("127.0.0.1::%d/%s", port.InternalPort, proto))
	}
	envFile, err := writeEnvFile(s.WorkspacePath, s.Environment)
	if err != nil {
		return ContainerState{}, err
	}
	if envFile != "" {
		defer os.Remove(envFile)
		args = append(args, "--env-file", envFile)
	}
	args = append(args, s.Image)
	args = append(args, s.Command...)
	id, stderr, err := runCLI(ctx, p.Executable, args...)
	if err != nil {
		return ContainerState{}, fmt.Errorf("run container: %v: %s", err, stderr)
	}
	return ContainerState{Name: name, ID: strings.TrimSpace(id), Status: "running", SpecHash: hash}, nil
}
func (e *CLIEngine) InspectContainer(ctx context.Context, runtimeID, applicationID string) (ContainerState, error) {
	p, err := e.Probe(ctx)
	if err != nil {
		return ContainerState{}, err
	}
	name := containerName(runtimeID, applicationID)
	out, stderr, err := runCLI(ctx, p.Executable, "container", "inspect", name)
	if err != nil {
		if containerNotFound(stderr) {
			return ContainerState{Name: name, Status: "absent", RuntimeID: runtimeID, ApplicationID: applicationID}, nil
		}
		return ContainerState{}, fmt.Errorf("inspect container: %v: %s", err, stderr)
	}
	var raw []struct {
		ID    string `json:"Id"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		HostConfig struct {
			ReadonlyRootfs bool     `json:"ReadonlyRootfs"`
			NetworkMode    string   `json:"NetworkMode"`
			Privileged     bool     `json:"Privileged"`
			CapAdd         []string `json:"CapAdd"`
			CapDrop        []string `json:"CapDrop"`
			SecurityOpt    []string `json:"SecurityOpt"`
		} `json:"HostConfig"`
		NetworkSettings struct {
			Ports map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
		Mounts []struct {
			Type        string `json:"Type"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
			RW          bool   `json:"RW"`
		} `json:"Mounts"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil || len(raw) != 1 {
		return ContainerState{}, fmt.Errorf("inspect container response: %w", ErrInvalidInput)
	}
	x := raw[0]
	st := ContainerState{Name: name, ID: x.ID, Status: strings.ToLower(x.State.Status), Image: x.Config.Image,
		ReadOnlyRootFS: x.HostConfig.ReadonlyRootfs, NetworkMode: strings.ToLower(x.HostConfig.NetworkMode),
		Privileged: x.HostConfig.Privileged, CapAdd: append([]string(nil), x.HostConfig.CapAdd...), CapDrop: append([]string(nil), x.HostConfig.CapDrop...), SecurityOpt: append([]string(nil), x.HostConfig.SecurityOpt...)}
	if x.Config.Labels != nil {
		st.SpecHash = x.Config.Labels["io.harness.spec-hash"]
		st.RuntimeID = x.Config.Labels["io.harness.runtime-id"]
		st.ApplicationID = x.Config.Labels["io.harness.application-id"]
	}
	bindsSafe := true
	for _, m := range x.Mounts {
		ms := MountState{Type: strings.ToLower(m.Type), Source: m.Source, Destination: m.Destination, RW: m.RW}
		st.Mounts = append(st.Mounts, ms)
		if ms.Type == "bind" && (ms.Destination != "/workspace" || !ms.RW) {
			bindsSafe = false
		}
	}
	securityOK := false
	for _, opt := range st.SecurityOpt {
		if strings.Contains(strings.ToLower(opt), "no-new-privileges") {
			securityOK = true
			break
		}
	}
	portsSafe := true
	for key, bindings := range x.NetworkSettings.Ports {
		parts := strings.SplitN(key, "/", 2)
		internal, _ := strconv.Atoi(parts[0])
		proto := "tcp"
		if len(parts) == 2 {
			proto = strings.ToLower(parts[1])
		}
		for _, b := range bindings {
			hp, _ := strconv.Atoi(b.HostPort)
			ps := PortState{InternalPort: internal, Protocol: proto, HostIP: b.HostIP, HostPort: hp}
			st.Ports = append(st.Ports, ps)
			if b.HostIP != "127.0.0.1" || hp <= 0 {
				portsSafe = false
			}
		}
	}
	capDropAll := false
	for _, c := range st.CapDrop {
		if strings.EqualFold(c, "ALL") {
			capDropAll = true
			break
		}
	}
	network, netErr := e.InspectNetwork(ctx, runtimeID)
	networkOwned := netErr == nil && network.RuntimeID == runtimeID
	st.NetworkInternal = networkOwned && network.Internal
	labelsOK := st.RuntimeID == runtimeID && st.ApplicationID == applicationID
	st.IsolationVerified = labelsOK && st.ReadOnlyRootFS && st.NetworkMode == strings.ToLower(runtimeNetworkName(runtimeID)) && networkOwned && !st.Privileged && len(st.CapAdd) == 0 && capDropAll && securityOK && bindsSafe && portsSafe
	return st, nil
}

func (e *CLIEngine) ListRuntime(ctx context.Context, runtimeID string) ([]ContainerState, error) {
	p, err := e.Probe(ctx)
	if err != nil {
		return nil, err
	}
	out, stderr, err := runCLI(ctx, p.Executable, "ps", "-a", "--filter", "label=io.harness.runtime-id="+runtimeID, "--format", `{{.Names}}|{{.Status}}|{{.ID}}`)
	if err != nil {
		return nil, fmt.Errorf("list runtime containers: %v: %s", err, stderr)
	}
	if strings.TrimSpace(out) == "" {
		return []ContainerState{}, nil
	}
	lines := strings.Split(out, "\n")
	states := make([]ContainerState, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		st := ContainerState{Name: parts[0]}
		if len(parts) > 1 {
			v := strings.ToLower(parts[1])
			if strings.HasPrefix(v, "up ") {
				st.Status = "running"
			} else if strings.Contains(v, "exited") {
				st.Status = "stopped"
			} else {
				st.Status = v
			}
		}
		if len(parts) > 2 {
			st.ID = parts[2]
		}
		states = append(states, st)
	}
	return states, nil
}
func (e *CLIEngine) ExecContainer(ctx context.Context, runtimeID, applicationID string, command []string) (ExecResult, error) {
	if len(command) == 0 || len(command) > 128 {
		return ExecResult{}, ErrInvalidInput
	}
	for _, arg := range command {
		if strings.ContainsRune(arg, '\x00') {
			return ExecResult{}, ErrInvalidInput
		}
	}
	p, err := e.Probe(ctx)
	if err != nil {
		return ExecResult{}, err
	}
	name := containerName(runtimeID, applicationID)
	args := append([]string{"exec", name}, command...)
	stdout, stderr, err := runCLI(ctx, p.Executable, args...)
	if err != nil {
		return ExecResult{Stdout: stdout, Stderr: stderr}, fmt.Errorf("exec sandbox command: %v: %s", err, stderr)
	}
	return ExecResult{Stdout: stdout, Stderr: stderr}, nil
}

func (e *CLIEngine) StopContainer(ctx context.Context, runtimeID, applicationID string) (ContainerState, error) {
	p, err := e.Probe(ctx)
	if err != nil {
		return ContainerState{}, err
	}
	name := containerName(runtimeID, applicationID)
	out, stderr, err := runCLI(ctx, p.Executable, "container", "inspect", name, "--format", `{{.State.Status}}|{{.Id}}`)
	if err != nil {
		if containerNotFound(stderr) {
			return ContainerState{Name: name, Status: "absent"}, nil
		}
		return ContainerState{}, fmt.Errorf("inspect container: %v: %s", err, stderr)
	}
	parts := strings.SplitN(out, "|", 2)
	id := ""
	status := ""
	if len(parts) > 0 {
		status = parts[0]
	}
	if len(parts) > 1 {
		id = parts[1]
	}
	if status == "running" {
		if _, stderr, err := runCLI(ctx, p.Executable, "stop", "--time", "10", name); err != nil {
			return ContainerState{}, fmt.Errorf("stop container: %v: %s", err, stderr)
		}
		status = "stopped"
	}
	return ContainerState{Name: name, ID: id, Status: status}, nil
}
func (e *CLIEngine) StopRuntime(ctx context.Context, runtimeID string) ([]ContainerState, error) {
	p, err := e.Probe(ctx)
	if err != nil {
		return nil, err
	}
	states, err := e.ListRuntime(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	for i := range states {
		if states[i].Status != "running" {
			continue
		}
		if _, stderr, err := runCLI(ctx, p.Executable, "stop", "--time", "10", states[i].Name); err != nil && !strings.Contains(strings.ToLower(stderr), "not running") {
			return states, fmt.Errorf("stop runtime container %s: %v: %s", states[i].Name, err, stderr)
		}
		states[i].Status = "stopped"
	}
	return states, nil
}
func containerName(runtimeID, appID string) string {
	return trimName("harness-"+sanitize(runtimeID)+"-"+sanitize(appID), 63)
}
func sanitize(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-_")
}
func trimName(v string, n int) string {
	if len(v) <= n {
		return v
	}
	return v[:n]
}
func specHash(s ContainerSpec) string {
	type hashSpec struct {
		RuntimeID           string            `json:"runtime_id"`
		ApplicationID       string            `json:"application_id"`
		Image               string            `json:"image"`
		WorkspacePath       string            `json:"workspace_path"`
		Command             []string          `json:"command,omitempty"`
		WorkingDir          string            `json:"working_dir,omitempty"`
		EnvironmentIdentity map[string]string `json:"environment_identity,omitempty"`
		Ports               []PortSpec        `json:"ports,omitempty"`
		Limits              ResourceLimits    `json:"limits"`
		NetworkInternal     bool              `json:"network_internal"`
	}
	b, _ := json.Marshal(hashSpec{RuntimeID: s.RuntimeID, ApplicationID: s.ApplicationID, Image: s.Image, WorkspacePath: s.WorkspacePath, Command: s.Command, WorkingDir: s.WorkingDir, EnvironmentIdentity: s.EnvironmentIdentity, Ports: s.Ports, Limits: s.Limits, NetworkInternal: s.NetworkInternal})
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func runtimeNetworkName(runtimeID string) string {
	return trimName("harness-"+sanitize(runtimeID)+"-net", 63)
}

// writeEnvFile stages credentials outside the container-mounted Workspace,
// under the configured OnePane runtime root instead of global/system TMP.
// Only the trusted rootless service account may read the 0700 staging folder.
func writeEnvFile(workspacePath string, env map[string]string) (string, error) {
 workspacePath,err:=filepath.Abs(workspacePath)
 if err!=nil{return "",err}
 root:=filepath.Dir(workspacePath)
 for _,path:=range []string{root,workspacePath}{
  st,err:=os.Lstat(path)
  if err!=nil{return "",err}
  if !st.IsDir()||st.Mode()&os.ModeSymlink!=0{return "",ErrInvalidInput}
 }
 staging:=filepath.Join(root,".onepane-private-env")
 if err:=os.Mkdir(staging,0o700);err!=nil&&!os.IsExist(err){return "",err}
 info,err:=os.Lstat(staging)
 if err!=nil{return "",err}
 if !info.IsDir()||info.Mode()&os.ModeSymlink!=0||info.Mode().Perm()&0o077!=0{
  return "",ErrInvalidInput
 }
 // Apply retention even when this particular tool has no secret bindings:
 // a previous process crash must not leave old credentials indefinitely.
 if err:=cleanupStaleEnvFiles(staging,time.Now());err!=nil{return "",err}
 if len(env)==0{return "",nil}
 keys:=make([]string,0,len(env))
 for key:=range env{keys=append(keys,key)}
 sort.Strings(keys)
 f,err:=os.CreateTemp(staging,"onepane-sandbox-env-*")
 if err!=nil{return "",err}
 path:=f.Name()
 cleanup:=func(){_=f.Close();_=os.Remove(path)}
 if err=f.Chmod(0o600);err!=nil{cleanup();return "",err}
 for _,k:=range keys{
  v:=env[k]
  if strings.ContainsAny(k,"=\x00\r\n")||strings.ContainsAny(v,"\x00\r\n"){
   cleanup();return "",ErrInvalidInput
  }
  if _,err=f.WriteString(k+"="+v+"\n");err!=nil{cleanup();return "",err}
 }
 if err=f.Close();err!=nil{_=os.Remove(path);return "",err}
 return path,nil
}
const sandboxEnvStaleAfter = 24 * time.Hour

// cleanupStaleEnvFiles only removes old regular files matching OnePane's
// private env-file prefix from the already validated 0700 runtime staging
// directory. It never traverses subdirectories, follows links, or edits user
// Project/Workspace files. Recent files remain untouched for active engines.
func cleanupStaleEnvFiles(dir string, now time.Time) error {
 entries,err:=os.ReadDir(dir)
 if err!=nil{return err}
 for _,entry:=range entries {
  name:=entry.Name()
  if !strings.HasPrefix(name,"onepane-sandbox-env-"){continue}
  path:=filepath.Join(dir,name)
  info,err:=os.Lstat(path)
  if os.IsNotExist(err){continue}
  if err!=nil{return err}
  if !info.Mode().IsRegular(){continue}
  if now.Sub(info.ModTime())<sandboxEnvStaleAfter{continue}
  if err:=os.Remove(path);err!=nil&&!os.IsNotExist(err){return err}
 }
 return nil
}

func containerNotFound(stderr string) bool {
	v := strings.ToLower(stderr)
	return strings.Contains(v, "no such container") || strings.Contains(v, "no container with name") || strings.Contains(v, "does not exist") || strings.Contains(v, "not found")
}
