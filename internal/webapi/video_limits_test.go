package webapi

import (
 "testing"
 "github.com/beruseruko/secretary/internal/core"
)

func TestVideoDefaultTwoFPSAndOneHundredFortyFrames(t *testing.T) {
 options := (VideoOptions{}).Defaults()
 if options.FPS != 2 || options.MaxFrames != core.MaxVideoFrames || options.MaxDurationSeconds != 70 {
  t.Fatalf("unexpected video defaults: %+v", options)
 }
 if err:=options.Validate();err!=nil{t.Fatal(err)}
 options.MaxFrames=core.MaxVideoFrames+1
 if err:=options.Validate();err==nil{t.Fatal("141 video frames accepted")}
 options.MaxFrames=core.MaxVideoFrames
 options.FPS=5
 if err:=options.Validate();err==nil{t.Fatal("unsupported FPS accepted")}
}
