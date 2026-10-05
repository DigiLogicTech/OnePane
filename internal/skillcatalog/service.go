package skillcatalog

import (
    "archive/zip"
    "bytes"
    "context"
    "crypto/sha256"
    "database/sql"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"

    "github.com/DigiLogicTech/OnePane/internal/clock"
    "github.com/DigiLogicTech/OnePane/internal/id"
    "github.com/DigiLogicTech/OnePane/internal/storage"
)

const MaxPackageBytes = 25 << 20

type Manifest struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Version string `json:"version"`
    Publisher string `json:"publisher,omitempty"`
    Description string `json:"description,omitempty"`
    Type string `json:"type,omitempty"`
    Requires struct {
        ContextTokens int64 `json:"context_tokens,omitempty"`
        Capabilities []string `json:"capabilities,omitempty"`
    } `json:"requires,omitempty"`
    ToolBundles []string `json:"tool_bundles,omitempty"`
    CompatibleRoles []string `json:"compatible_roles,omitempty"`
}

type Package struct {
    ID string `json:"id"`
    SkillID string `json:"skill_id"`
    Version string `json:"version"`
    Name string `json:"name"`
    Publisher string `json:"publisher"`
    SHA256 string `json:"package_sha256"`
    SourceKind string `json:"source_kind"`
    TrustState string `json:"trust_state"`
    Status string `json:"status"`
    Manifest json.RawMessage `json:"manifest"`
    Revision int64 `json:"revision"`
    CreatedAt int64 `json:"created_at"`
    UpdatedAt int64 `json:"updated_at"`
}

type ToolBundle struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Description string `json:"description"`
    SourceKind string `json:"source_kind"`
    Status string `json:"status"`
    Metadata json.RawMessage `json:"metadata"`
    Tools []string `json:"tools"`
}

type Assignment struct {
    ID string `json:"id"`
    WorkspaceID *string `json:"workspace_id,omitempty"`
    SkillPackageID string `json:"skill_package_id"`
    SubjectKind string `json:"subject_kind"`
    SubjectID string `json:"subject_id"`
    Enabled bool `json:"enabled"`
    Configuration json.RawMessage `json:"configuration"`
    CreatedAt int64 `json:"created_at"`
    UpdatedAt int64 `json:"updated_at"`
}

type TeamPreset struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Description string `json:"description"`
    SourceKind string `json:"source_kind"`
    Status string `json:"status"`
    Configuration json.RawMessage `json:"configuration"`
    Revision int64 `json:"revision"`
}

type Service struct {
    db *sql.DB
    tx storage.Transactor
    clock clock.Clock
    ids id.Generator
    root string
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, dataDir string) *Service {
    return &Service{db:db,tx:tx,clock:clk,ids:id.Generator{},root:filepath.Join(dataDir,"skills")}
}

func cleanManifest(raw []byte) (Manifest,json.RawMessage,error) {
    var m Manifest
    if err:=json.Unmarshal(raw,&m);err!=nil{return m,nil,fmt.Errorf("decode skill manifest: %w",err)}
    m.ID=strings.TrimSpace(m.ID);m.Name=strings.TrimSpace(m.Name);m.Version=strings.TrimSpace(m.Version);m.Publisher=strings.TrimSpace(m.Publisher)
    if m.ID==""||m.Name==""||m.Version==""{return m,nil,errors.New("skill manifest requires id, name and version")}
    if len(m.ToolBundles)>64||len(m.CompatibleRoles)>64||len(m.Requires.Capabilities)>64{return m,nil,errors.New("skill manifest contains too many capability entries")}
    out,_:=json.Marshal(m)
    return m,out,nil
}

func inspectArchive(raw []byte)(Manifest,json.RawMessage,error){
    if len(raw)==0||len(raw)>MaxPackageBytes{return Manifest{},nil,errors.New("skill package must be between 1 byte and 25 MiB")}
    zr,err:=zip.NewReader(bytes.NewReader(raw),int64(len(raw)));if err!=nil{return Manifest{},nil,errors.New("skill package must be a valid .opskill ZIP archive")}
    var manifest []byte
    for _,f:=range zr.File{
        name:=filepath.ToSlash(f.Name)
        clean:=filepath.ToSlash(filepath.Clean(name))
        if strings.HasPrefix(clean,"../")||strings.HasPrefix(name,"/")||clean==".."{return Manifest{},nil,errors.New("skill package contains an unsafe path")}
        if f.FileInfo().Mode()&os.ModeSymlink!=0{return Manifest{},nil,errors.New("skill package symlinks are not allowed")}
        if clean=="manifest.json"{
            if f.UncompressedSize64>1<<20{return Manifest{},nil,errors.New("skill manifest exceeds 1 MiB")}
            rc,e:=f.Open();if e!=nil{return Manifest{},nil,e}
            manifest,e=io.ReadAll(io.LimitReader(rc,1<<20));_ = rc.Close();if e!=nil{return Manifest{},nil,e}
        }
    }
    if len(manifest)==0{return Manifest{},nil,errors.New("skill package is missing root manifest.json")}
    return cleanManifest(manifest)
}

func scanPackage(row interface{Scan(...any)error})(Package,error){
    var p Package;var manifest string
    err:=row.Scan(&p.ID,&p.SkillID,&p.Version,&p.Name,&p.Publisher,&p.SHA256,&p.SourceKind,&p.TrustState,&p.Status,&manifest,&p.Revision,&p.CreatedAt,&p.UpdatedAt)
    p.Manifest=json.RawMessage(manifest);return p,err
}

func (s *Service) Upload(ctx context.Context, raw []byte, actor string)(Package,error){
    m,manifest,err:=inspectArchive(raw);if err!=nil{return Package{},err}
    actor=strings.TrimSpace(actor);if actor==""{return Package{},errors.New("actor is required")}
    sum:=sha256.Sum256(raw);sha:=hex.EncodeToString(sum[:]);pid,_:=s.ids.New("skillpkg");now:=s.clock.UnixMilli()
    quarantine:=filepath.Join(s.root,"quarantine");if err:=os.MkdirAll(quarantine,0o700);err!=nil{return Package{},err}
    path:=filepath.Join(quarantine,sha+".opskill")
    if err:=os.WriteFile(path,raw,0o600);err!=nil{return Package{},err}
    _,err=s.db.ExecContext(ctx,`INSERT INTO skill_packages(id,skill_id,version,name,publisher,package_sha256,package_path,source_kind,trust_state,status,manifest_json,requested_by,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'uploaded','unverified','quarantined',?,?,1,?,?)`,
        pid,m.ID,m.Version,m.Name,m.Publisher,sha,path,string(manifest),actor,now,now)
    if err!=nil{_ = os.Remove(path);return Package{},err}
    return s.Package(ctx,pid)
}

func (s *Service) Package(ctx context.Context,idv string)(Package,error){
    return scanPackage(s.db.QueryRowContext(ctx,`SELECT id,skill_id,version,name,publisher,package_sha256,source_kind,trust_state,status,manifest_json,revision,created_at,updated_at FROM skill_packages WHERE id=?`,strings.TrimSpace(idv)))
}
func (s *Service) Packages(ctx context.Context)([]Package,error){
    rows,err:=s.db.QueryContext(ctx,`SELECT id,skill_id,version,name,publisher,package_sha256,source_kind,trust_state,status,manifest_json,revision,created_at,updated_at FROM skill_packages WHERE status<>'removed' ORDER BY name,version`);if err!=nil{return nil,err};defer rows.Close()
    out:=[]Package{};for rows.Next(){p,e:=scanPackage(rows);if e!=nil{return nil,e};out=append(out,p)};return out,rows.Err()
}
func (s *Service) Install(ctx context.Context,idv,actor string)(Package,error){
    var path,sha,status string
    if err:=s.db.QueryRowContext(ctx,`SELECT package_path,package_sha256,status FROM skill_packages WHERE id=?`,idv).Scan(&path,&sha,&status);err!=nil{return Package{},err}
    if status!="quarantined"&&status!="disabled"{return Package{},errors.New("skill package is not awaiting installation")}
    raw,err:=os.ReadFile(path);if err!=nil{return Package{},err};sum:=sha256.Sum256(raw);if hex.EncodeToString(sum[:])!=sha{return Package{},errors.New("skill package hash changed after quarantine")}
    _,_,err=inspectArchive(raw);if err!=nil{return Package{},err}
    installed:=filepath.Join(s.root,"installed");if err:=os.MkdirAll(installed,0o700);err!=nil{return Package{},err}
    dst:=filepath.Join(installed,sha+".opskill");if err:=os.WriteFile(dst,raw,0o600);err!=nil{return Package{},err}
    now:=s.clock.UnixMilli();_,err=s.db.ExecContext(ctx,`UPDATE skill_packages SET package_path=?,status='installed',revision=revision+1,updated_at=? WHERE id=?`,dst,now,idv);if err!=nil{return Package{},err}
    if path!=dst{_ = os.Remove(path)}
    return s.Package(ctx,idv)
}
func (s *Service) SetStatus(ctx context.Context,idv,status string)(Package,error){
    if status!="disabled"&&status!="installed"&&status!="removed"{return Package{},errors.New("invalid skill package status")}
    now:=s.clock.UnixMilli();if _,err:=s.db.ExecContext(ctx,`UPDATE skill_packages SET status=?,revision=revision+1,updated_at=? WHERE id=?`,status,now,idv);err!=nil{return Package{},err};return s.Package(ctx,idv)
}
func (s *Service) ToolBundles(ctx context.Context)([]ToolBundle,error){
    rows,err:=s.db.QueryContext(ctx,`SELECT id,name,description,source_kind,status,metadata_json FROM tool_bundles WHERE status<>'archived' ORDER BY source_kind,name`);if err!=nil{return nil,err};defer rows.Close()
    out:=[]ToolBundle{};for rows.Next(){var x ToolBundle;var meta string;if err:=rows.Scan(&x.ID,&x.Name,&x.Description,&x.SourceKind,&x.Status,&meta);err!=nil{return nil,err};x.Metadata=json.RawMessage(meta);tr,e:=s.db.QueryContext(ctx,`SELECT tool_id FROM tool_bundle_tools WHERE bundle_id=? ORDER BY tool_id`,x.ID);if e!=nil{return nil,e};for tr.Next(){var t string;if tr.Scan(&t)==nil{x.Tools=append(x.Tools,t)}};_ = tr.Close();out=append(out,x)};return out,rows.Err()
}
func (s *Service) Assignments(ctx context.Context,workspaceID string)([]Assignment,error){
    rows,err:=s.db.QueryContext(ctx,`SELECT id,workspace_id,skill_package_id,subject_kind,subject_id,enabled,configuration_json,created_at,updated_at FROM skill_assignments WHERE workspace_id IS NULL OR workspace_id=? ORDER BY subject_kind,subject_id`,workspaceID);if err!=nil{return nil,err};defer rows.Close()
    out:=[]Assignment{};for rows.Next(){var x Assignment;var ws sql.NullString;var en int;var cfg string;if err:=rows.Scan(&x.ID,&ws,&x.SkillPackageID,&x.SubjectKind,&x.SubjectID,&en,&cfg,&x.CreatedAt,&x.UpdatedAt);err!=nil{return nil,err};if ws.Valid{x.WorkspaceID=&ws.String};x.Enabled=en!=0;x.Configuration=json.RawMessage(cfg);out=append(out,x)};return out,rows.Err()
}
func (s *Service) Assign(ctx context.Context,workspaceID,packageID,kind,subjectID,actor string,cfg json.RawMessage)(Assignment,error){
    switch kind{case "agent_profile","team","team_member","workspace":default:return Assignment{},errors.New("invalid skill assignment subject")}
    if len(cfg)==0{cfg=json.RawMessage(`{}`)};if !json.Valid(cfg){return Assignment{},errors.New("invalid assignment configuration")}
    idv,_:=s.ids.New("skillassign");now:=s.clock.UnixMilli()
    _,err:=s.db.ExecContext(ctx,`INSERT INTO skill_assignments(id,workspace_id,skill_package_id,subject_kind,subject_id,enabled,configuration_json,assigned_by,created_at,updated_at) VALUES(?,?,?,?,?,1,?,?,?,?) ON CONFLICT(workspace_id,skill_package_id,subject_kind,subject_id) DO UPDATE SET enabled=1,configuration_json=excluded.configuration_json,assigned_by=excluded.assigned_by,updated_at=excluded.updated_at`,idv,workspaceID,packageID,kind,subjectID,string(cfg),actor,now,now);if err!=nil{return Assignment{},err}
    var x Assignment;var ws sql.NullString;var en int;var raw string
    err=s.db.QueryRowContext(ctx,`SELECT id,workspace_id,skill_package_id,subject_kind,subject_id,enabled,configuration_json,created_at,updated_at FROM skill_assignments WHERE workspace_id=? AND skill_package_id=? AND subject_kind=? AND subject_id=?`,workspaceID,packageID,kind,subjectID).Scan(&x.ID,&ws,&x.SkillPackageID,&x.SubjectKind,&x.SubjectID,&en,&raw,&x.CreatedAt,&x.UpdatedAt)
    if ws.Valid{x.WorkspaceID=&ws.String};x.Enabled=en!=0;x.Configuration=json.RawMessage(raw);return x,err
}
func (s *Service) TeamPresets(ctx context.Context)([]TeamPreset,error){
    rows,err:=s.db.QueryContext(ctx,`SELECT id,name,description,source_kind,configuration_json,status,revision FROM team_presets WHERE status='active' ORDER BY source_kind,name`);if err!=nil{return nil,err};defer rows.Close()
    out:=[]TeamPreset{};for rows.Next(){var x TeamPreset;var cfg string;if err:=rows.Scan(&x.ID,&x.Name,&x.Description,&x.SourceKind,&cfg,&x.Status,&x.Revision);err!=nil{return nil,err};x.Configuration=json.RawMessage(cfg);out=append(out,x)};return out,rows.Err()
}
