package runtimeendpoint

import (
 "encoding/json"
 "testing"
)

func TestLiteralLoopbackHTTPClassifiesOnlyExplicitIP(t *testing.T){
 for _,tc:=range []struct{value string;want bool}{
  {`{"base_url":"http://127.0.0.1:9000"}`,true},
  {`{"base_url":"https://[::1]:9000/v1"}`,true},
  {`{"base_url":"http://127.5.6.7"}`,true},
  {`{"base_url":"http://localhost:9000"}`,false},
  {`{"base_url":"http://localHost.:9000"}`,false},
  {`{"base_url":"http://127.0.0.1.evil.test:9000"}`,false},
  {`{"base_url":"http://127.0.0.1@evil.test:9000"}`,false},
  {`{"base_url":"http://192.168.1.1:9000"}`,false},
  {`{"base_url":"http://10.0.0.9:9000"}`,false},
  {`{"base_url":"http://[2001:db8::9]:9000"}`,false},
  {`{"base_url":"https://example.com"}`,false},
  {`{"base_url":"http://127.0.0.1:9000?q=1"}`,false},
  {`{"base_url":"http://127.0.0.1:9000#frag"}`,false},
  {`{"base_url":"http://127.0.0.1:bad"}`,false},
  {`{"base_url":"http://127.0.0.1:9000 ","other":true}`,false},
  {`{"base_url":null}`,false},
  {`{"base_url":1234}`,false},
  {`{"base_url":"//127.0.0.1:9000"}`,false},
  {`{}`,false},
  {`null`,false},
  {`{`,false},
 }{
  t.Run(tc.value,func(t *testing.T){
   got:=LiteralLoopbackHTTP(json.RawMessage(tc.value))
   if got!=tc.want{t.Fatalf("url %s local=%v want %v",tc.value,got,tc.want)}
  })
 }
}
