package projectworkspace

import (
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

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type ProjectStorageLocation struct {
	ProjectID string `json:"project_id"`
	StorageRoot string `json:"storage_root"`
	PreviousRoot *string `json:"previous_root,omitempty"`
	MoveState string `json:"move_state"`
	MoveDetail json.RawMessage `json:"move_detail"`
	Revision int64 `json:"revision"`
	UpdatedAt int64 `json:"updated_at"`
}

func (s *Service) ConfigureProjectStorageRoot(root string) error {
	root=strings.TrimSpace(root); if root==""{return errors.New("project storage root required")}
	abs,err:=filepath.Abs(root);if err!=nil{return err};s.projectRoot=filepath.Clean(abs);return nil
}
func (s *Service) defaultProjectStorage(projectID string) string { root:=strings.TrimSpace(s.projectRoot);if root==""{root=filepath.Join(os.TempDir(),"OnePane","Projects")};return filepath.Join(root,projectID) }
func (s *Service) ensureProjectStorage(ctx context.Context, projectID string)(ProjectStorageLocation,error){
	var x ProjectStorageLocation;var previous sql.NullString;var detail string
	err:=s.db.QueryRowContext(ctx,`SELECT project_id,storage_root,previous_root,move_state,move_detail_json,revision,updated_at FROM project_storage_locations WHERE project_id=?`,projectID).Scan(&x.ProjectID,&x.StorageRoot,&previous,&x.MoveState,&detail,&x.Revision,&x.UpdatedAt)
	if err==nil{if previous.Valid{v:=previous.String;x.PreviousRoot=&v};x.MoveDetail=json.RawMessage(detail);return x,nil}
	if !errors.Is(err,sql.ErrNoRows){return x,err}
	if _,err:=s.repo.Project(ctx,projectID);err!=nil{return x,err}
	root:=s.defaultProjectStorage(projectID);if err:=os.MkdirAll(root,0o750);err!=nil{return x,err};if err:=ensureProjectMarker(root,projectID);err!=nil{return x,err}
	now:=s.clock.UnixMilli();if _,err:=s.db.ExecContext(ctx,`INSERT INTO project_storage_locations(project_id,storage_root,move_state,move_detail_json,revision,updated_at) VALUES(?,?,'ready','{}',1,?) ON CONFLICT(project_id) DO NOTHING`,projectID,root,now);err!=nil{return x,err}
	return s.ensureProjectStorage(ctx,projectID)
}
func (s *Service) ProjectStorage(ctx context.Context,projectID string)(ProjectStorageLocation,error){projectID=strings.TrimSpace(projectID);if s==nil||s.db==nil||projectID==""{return ProjectStorageLocation{},ErrInvalidCommand};return s.ensureProjectStorage(ctx,projectID)}

func (s *Service) MoveProjectStorage(ctx context.Context, projectID,newRoot,actor string)(ProjectStorageLocation,error){
	projectID,newRoot,actor=strings.TrimSpace(projectID),strings.TrimSpace(newRoot),strings.TrimSpace(actor)
	if projectID==""||newRoot==""||actor==""{return ProjectStorageLocation{},ErrInvalidCommand}
	abs,err:=filepath.Abs(newRoot);if err!=nil{return ProjectStorageLocation{},err};newRoot=filepath.Clean(abs)
	s.storageMu.Lock();defer s.storageMu.Unlock()
	p,err:=s.repo.Project(ctx,projectID);if err!=nil{return ProjectStorageLocation{},err}
	current,err:=s.ensureProjectStorage(ctx,projectID);if err!=nil{return ProjectStorageLocation{},err}
	if samePath(current.StorageRoot,newRoot){return current,nil}
	if isPathWithin(newRoot,current.StorageRoot)||isPathWithin(current.StorageRoot,newRoot){return ProjectStorageLocation{},errors.New("new Project storage must not be inside the current storage tree (or vice versa)")}
	now:=s.clock.UnixMilli();detail,_:=json.Marshal(map[string]any{"from":current.StorageRoot,"to":newRoot,"phase":"preparing"})
	res,err:=s.db.ExecContext(ctx,`UPDATE project_storage_locations SET move_state='preparing',previous_root=storage_root,move_detail_json=?,revision=revision+1,updated_at=? WHERE project_id=? AND revision=? AND move_state IN ('ready','failed')`,string(detail),now,projectID,current.Revision)
	if err!=nil{return ProjectStorageLocation{},err};if n,_:=res.RowsAffected();n!=1{return ProjectStorageLocation{},errors.New("Project storage is already being moved")}
	staging:=newRoot+".onepane-moving";_=os.RemoveAll(staging)
	if err:=os.MkdirAll(filepath.Dir(newRoot),0o750);err!=nil{_=s.failProjectStorageMove(ctx,projectID,current.StorageRoot,newRoot,err);return ProjectStorageLocation{},err}
	if entries,e:=os.ReadDir(newRoot);e==nil&&len(entries)>0{err=errors.New("destination Project storage directory is not empty");_=s.failProjectStorageMove(ctx,projectID,current.StorageRoot,newRoot,err);return ProjectStorageLocation{},err}else if e!=nil&&!errors.Is(e,os.ErrNotExist){_=s.failProjectStorageMove(ctx,projectID,current.StorageRoot,newRoot,e);return ProjectStorageLocation{},e}
	_=os.RemoveAll(newRoot)
	_,_=s.db.ExecContext(ctx,`UPDATE project_storage_locations SET move_state='copying',move_detail_json=?,updated_at=? WHERE project_id=?`,string(detail),s.clock.UnixMilli(),projectID)
	manifest,err:=copyTreeWithManifest(ctx,current.StorageRoot,staging);if err!=nil{_=os.RemoveAll(staging);_=s.failProjectStorageMove(ctx,projectID,current.StorageRoot,newRoot,err);return ProjectStorageLocation{},err}
	_,_=s.db.ExecContext(ctx,`UPDATE project_storage_locations SET move_state='verifying',updated_at=? WHERE project_id=?`,s.clock.UnixMilli(),projectID)
	if err:=ensureProjectMarker(staging,projectID);err!=nil{_=os.RemoveAll(staging);_=s.failProjectStorageMove(ctx,projectID,current.StorageRoot,newRoot,err);return ProjectStorageLocation{},err}
	if err:=verifyTreeManifest(ctx,staging,manifest);err!=nil{_=os.RemoveAll(staging);_=s.failProjectStorageMove(ctx,projectID,current.StorageRoot,newRoot,err);return ProjectStorageLocation{},err}
	if err:=os.Rename(staging,newRoot);err!=nil{_=os.RemoveAll(staging);_=s.failProjectStorageMove(ctx,projectID,current.StorageRoot,newRoot,err);return ProjectStorageLocation{},err}
	detail,_=json.Marshal(map[string]any{"from":current.StorageRoot,"to":newRoot,"phase":"verified","files":len(manifest)});now=s.clock.UnixMilli()
	err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
		res,err:=tx.ExecContext(ctx,`UPDATE project_storage_locations SET storage_root=?,previous_root=?,move_state='ready',move_detail_json=?,revision=revision+1,updated_at=? WHERE project_id=? AND storage_root=?`,newRoot,current.StorageRoot,string(detail),now,projectID,current.StorageRoot);if err!=nil{return err}
		if n,_:=res.RowsAffected();n!=1{return ErrRevisionConflict}
		eid,_:=s.ids.New("evt");payload,_:=json.Marshal(map[string]any{"project_id":projectID,"from":current.StorageRoot,"to":newRoot,"verified_files":len(manifest)})
		return s.events.Append(ctx,tx,event.Event{ID:eid,WorkspaceID:&p.WorkspaceID,Type:"project.storage_moved",AggregateType:"project",AggregateID:projectID,ActorPrincipalID:&actor,Payload:payload,OccurredAt:now})
	})
	if err!=nil{_=os.RemoveAll(newRoot);_=s.failProjectStorageMove(ctx,projectID,current.StorageRoot,newRoot,err);return ProjectStorageLocation{},err}
	if projectMarkerMatches(current.StorageRoot,projectID){_=os.RemoveAll(current.StorageRoot)}
	return s.ensureProjectStorage(ctx,projectID)
}
func(s *Service)failProjectStorageMove(ctx context.Context,projectID,oldRoot,newRoot string,cause error)error{detail,_:=json.Marshal(map[string]any{"from":oldRoot,"to":newRoot,"error":cause.Error()});_,err:=s.db.ExecContext(ctx,`UPDATE project_storage_locations SET move_state='failed',move_detail_json=?,revision=revision+1,updated_at=? WHERE project_id=?`,string(detail),s.clock.UnixMilli(),projectID);return err}

type treeManifestEntry struct{Relative string;Size int64;SHA256 string;Mode os.FileMode}
func copyTreeWithManifest(ctx context.Context,source,dest string)([]treeManifestEntry,error){
	if _,err:=os.Stat(source);errors.Is(err,os.ErrNotExist){if err:=os.MkdirAll(dest,0o750);err!=nil{return nil,err};return []treeManifestEntry{},nil}else if err!=nil{return nil,err}
	if err:=os.MkdirAll(dest,0o750);err!=nil{return nil,err};manifest:=[]treeManifestEntry{}
	err:=filepath.Walk(source,func(path string,info os.FileInfo,walkErr error)error{
		if walkErr!=nil{return walkErr};select{case<-ctx.Done():return ctx.Err();default:}
		rel,err:=filepath.Rel(source,path);if err!=nil||rel==".."||strings.HasPrefix(rel,".."+string(filepath.Separator)){return errors.New("project storage path escaped source root")}
		if rel=="."{return nil};target:=filepath.Join(dest,rel)
		if info.Mode()&os.ModeSymlink!=0{return fmt.Errorf("project storage contains unsupported symlink %s",rel)}
		if info.IsDir(){return os.MkdirAll(target,info.Mode().Perm())};if !info.Mode().IsRegular(){return fmt.Errorf("project storage contains unsupported file type %s",rel)}
		if err:=os.MkdirAll(filepath.Dir(target),0o750);err!=nil{return err};in,err:=os.Open(path);if err!=nil{return err};defer in.Close();out,err:=os.OpenFile(target,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,info.Mode().Perm());if err!=nil{return err}
		h:=sha256.New();n,cpErr:=io.Copy(io.MultiWriter(out,h),in);closeErr:=out.Close();if cpErr!=nil{return cpErr};if closeErr!=nil{return closeErr}
		manifest=append(manifest,treeManifestEntry{Relative:rel,Size:n,SHA256:hex.EncodeToString(h.Sum(nil)),Mode:info.Mode()});return nil
	});return manifest,err
}
func verifyTreeManifest(ctx context.Context,root string,manifest []treeManifestEntry)error{for _,item:=range manifest{select{case<-ctx.Done():return ctx.Err();default:};f,err:=os.Open(filepath.Join(root,item.Relative));if err!=nil{return err};h:=sha256.New();n,cpErr:=io.Copy(h,f);closeErr:=f.Close();if cpErr!=nil{return cpErr};if closeErr!=nil{return closeErr};if n!=item.Size||!strings.EqualFold(hex.EncodeToString(h.Sum(nil)),item.SHA256){return fmt.Errorf("verification failed for %s",item.Relative)}};return nil}
func ensureProjectMarker(root,projectID string)error{if strings.TrimSpace(root)==""||strings.TrimSpace(projectID)==""{return errors.New("invalid Project storage marker")};path:=filepath.Join(root,".onepane-project-storage");if b,err:=os.ReadFile(path);err==nil{if strings.TrimSpace(string(b))!=projectID{return errors.New("storage directory belongs to a different OnePane Project")};return nil}else if !errors.Is(err,os.ErrNotExist){return err};return os.WriteFile(path,[]byte(projectID+"\n"),0o600)}
func projectMarkerMatches(root,projectID string)bool{b,err:=os.ReadFile(filepath.Join(root,".onepane-project-storage"));return err==nil&&strings.TrimSpace(string(b))==strings.TrimSpace(projectID)}
func samePath(a,b string)bool{aa,_:=filepath.Abs(a);bb,_:=filepath.Abs(b);return strings.EqualFold(filepath.Clean(aa),filepath.Clean(bb))}
func isPathWithin(candidate,root string)bool{c,err1:=filepath.Abs(candidate);r,err2:=filepath.Abs(root);if err1!=nil||err2!=nil{return false};rel,err:=filepath.Rel(r,c);return err==nil&&rel!="."&&rel!=".."&&!strings.HasPrefix(rel,".."+string(filepath.Separator))}
