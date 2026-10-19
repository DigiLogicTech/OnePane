//go:build windows

package main

import (
 "errors"
 "fmt"
 "os"
 "path/filepath"

 "golang.org/x/sys/windows"
)

// verifySelectedRootComponents is read-only: check each existing selected
// storage ancestor before writing configuration, logs or SQLite. A junction
// elsewhere on a selected path could move a D: install onto C: unexpectedly.
// The default/legacy root is intentionally left alone unless MSI explicitly
// selected it, to avoid disrupting existing installations.
func verifySelectedRootComponents(root string) error {
 if !filepath.IsAbs(root){return fmt.Errorf("MSI-selected data root must be absolute")}
 clean:=filepath.Clean(root)
 for current:=clean; ; current=filepath.Dir(current) {
  info,err:=os.Lstat(current)
  if errors.Is(err,os.ErrNotExist){
   // New directories under an existing normal ancestor are fine.
  }else if err!=nil{
   return fmt.Errorf("inspect selected data-root ancestry: %w",err)
  }else{
   if !info.IsDir()||info.Mode()&os.ModeSymlink!=0||info.Mode()&os.ModeIrregular!=0{
    return fmt.Errorf("selected data root contains a non-directory or reparse ancestor")
   }
   utf,err:=windows.UTF16PtrFromString(current)
   if err!=nil{return fmt.Errorf("inspect selected data root: %w",err)}
   attr,err:=windows.GetFileAttributes(utf)
   if err!=nil{return fmt.Errorf("inspect selected data-root reparse attributes: %w",err)}
   if attr&windows.FILE_ATTRIBUTE_REPARSE_POINT!=0{
    return fmt.Errorf("selected data root contains an NTFS reparse/junction ancestor; explicit operator review required")
   }
  }
  parent:=filepath.Dir(current)
  if parent==current{break}
 }
 return nil
}
