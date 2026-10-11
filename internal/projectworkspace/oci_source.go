package projectworkspace

import "strings"

// pinnedOCIImageSource accepts only a full, immutable SHA-256 image reference.
// The general source validator separately rejects host paths/URLs and control
// characters; this guard prevents floating tags in new Workspace sandboxes.
func pinnedOCIImageSource(ref string) bool {
 parts:=strings.Split(ref,"@sha256:")
 if len(parts)!=2 || strings.TrimSpace(parts[0])=="" ||
  strings.Contains(parts[0],"@") || len(parts[1])!=64 {
  return false
 }
 for i:=0;i<len(parts[1]);i++{
  b:=parts[1][i]
  if !((b>='0'&&b<='9')||(b>='a'&&b<='f')||(b>='A'&&b<='F')){
   return false
  }
 }
 return true
}
