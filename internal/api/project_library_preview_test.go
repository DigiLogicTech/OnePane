package api

import (
 "bytes"
 "strings"
 "testing"
)

func TestWorkspaceLibraryPreviewMIMEAllowlist(t *testing.T) {
 cases:=[]struct{media string;allow bool}{
  {"text/plain",true},
  {"text/plain; charset=utf-8",true},
  {"text/markdown",true},
  {"text/html",true}, // safely served as text/plain and WebUI textContent
  {"text/x-python",true},
  {"application/json",true},
  {"application/xml",true},
  {"application/x-yaml",true},
  {"application/javascript",true},
  {"application/octet-stream",false},
  {"image/svg+xml",false},
  {"application/pdf",false},
  {"invalid",false},
  {"text/plain;;broken",false},
  {"",false},
 }
 for _,tt:=range cases {
  t.Run(tt.media,func(t *testing.T){
   if got:=previewableWorkspaceLibraryMIME(tt.media);got!=tt.allow{
    t.Fatalf("media type %q allowed=%v want %v",tt.media,got,tt.allow)
   }
  })
 }
}

func TestWorkspaceLibraryPreviewTextBudgetAndUTF8(t *testing.T) {
 for _,tt:=range []struct{name string;body []byte;expected int64;good bool}{
  {"text",[]byte("valid plain text"),16,true},
  {"html_literal",[]byte("<script>alert(1)</script>"),25,true},
  {"utf8",[]byte("Unicode café"),int64(len([]byte("Unicode café"))),true},
  {"empty",[]byte{},0,true},
  {"invalid_utf8",[]byte{0xff,0xfe},2,false},
  {"shorter_than_version",[]byte("abc"),4,false},
  {"longer_than_version",[]byte("abcd"),3,false},
  {"too_large",bytes.Repeat([]byte("x"),int(workspaceLibraryTextPreviewLimit)+1),workspaceLibraryTextPreviewLimit+1,false},
  {"negative_length",[]byte("hi"),-1,false},
 } {
  t.Run(tt.name,func(t *testing.T){
   data,err:=readWorkspaceLibraryPreview(bytes.NewReader(tt.body),tt.expected)
   if (err==nil)!=tt.good {t.Fatalf("read good=%v want=%v: %v",err==nil,tt.good,err)}
   if err==nil && !bytes.Equal(data,tt.body){t.Fatalf("preview returned modified bytes")}
  })
 }
 if _,err:=readWorkspaceLibraryPreview(strings.NewReader("a"),workspaceLibraryTextPreviewLimit+1);err==nil{
  t.Fatal("oversized version length was permitted")
 }
}
