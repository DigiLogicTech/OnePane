package sandboxrunner

import (
 "errors"
 "fmt"
 "os"
 "path/filepath"
)

// managedWorkspacePath only creates/verifies children under the configured
// OnePane data root. Never follow an existing symlink in the managed Project,
// runtime or workspace directories: that could bind a different host directory
// into an otherwise isolated OCI container.
func managedWorkspacePath(dataDir, runtimeID string, create bool) (string, bool, error) {
 root, err := filepath.Abs(dataDir)
 if err != nil { return "", false, err }
 if create {
  if err := os.MkdirAll(root, 0o700); err != nil { return "", false, err }
 }
 root, err = filepath.EvalSymlinks(root)
 if errors.Is(err, os.ErrNotExist) && !create { return "", false, nil }
 if err != nil { return "", false, err }
 path := root
 for _, part := range []string{"projects", runtimeID, "workspace"} {
  path = filepath.Join(path, part)
  if create {
   if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
    return "", false, err
   }
  }
  info, err := os.Lstat(path)
  if errors.Is(err, os.ErrNotExist) && !create { return "", false, nil }
  if err != nil { return "", false, err }
  if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
   return "", false, fmt.Errorf("%w: managed Workspace path is not a real directory: %s", ErrInvalidInput, path)
  }
 }
 return path, true, nil
}

// exactWorkspaceMount rejects a running sandbox with a missing workspace,
// a different host source, or an additional writable/host bind. The engine's
// generic /workspace mount validation alone cannot establish Workspace ownership.
func exactWorkspaceMount(state ContainerState, expectedPath string) bool {
 expected, err := filepath.Abs(expectedPath)
 if err != nil || expectedPath == "" { return false }
 count := 0
 for _, mount := range state.Mounts {
  switch mount.Type {
  case "tmpfs":
   // /tmp is the only additional ephemeral mount the managed CLI provisions.
   if mount.Destination != "/tmp" {return false}
  case "bind":
   count++
   if count > 1 || mount.Destination != "/workspace" || !mount.RW ||
    !filepath.IsAbs(mount.Source) || filepath.Clean(mount.Source) != expected {
    return false
   }
  default:
   // Named/anonymous volumes and other mount types are not part of the
   // approved runtime specification and cannot be silently inherited.
   return false
  }
 }
 return count == 1
}
