package webapi

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
)

type videoToolEntry struct {
 Version string
 SHA256 string
}
type videoToolManifest struct {
 Origin string
 RequiredEncoder string `json:"required_encoder"`
 FFmpeg videoToolEntry
 FFprobe videoToolEntry
}

func toolVersion(ctx context.Context, path, name string) (string,error) {
 output,err:=exec.CommandContext(ctx,path,"-version").Output()
 if err!=nil{return "",fmt.Errorf("%s: %w",name,err)}
 fields:=strings.Fields(strings.SplitN(string(output),"\n",2)[0])
 if len(fields)<3||fields[0]!=name||fields[1]!="version"{return "",fmt.Errorf("%s: invalid version output",name)}
 return fields[2],nil
}

func verifyManagedVideoTools(ctx context.Context,dir string) (string,string,error) {
 ffmpeg:=filepath.Join(dir,"bin","video-tools","ffmpeg")
 ffprobe:=filepath.Join(dir,"bin","video-tools","ffprobe")
 raw,err:=os.ReadFile(filepath.Join(dir,"VIDEO-TOOLS.json"))
 if err!=nil{return "","",fmt.Errorf("video tool manifest: %w",err)}
 var manifest videoToolManifest
 if err=json.Unmarshal(raw,&manifest);err!=nil{return "","",err}
 if manifest.Origin==""||manifest.RequiredEncoder!="libwebp"||manifest.FFmpeg.Version==""||manifest.FFmpeg.Version!=manifest.FFprobe.Version{return "","",errors.New("video tool manifest versions or origin invalid")}
 for _,entry:=range []struct{path,expected string}{{ffmpeg,manifest.FFmpeg.SHA256},{ffprobe,manifest.FFprobe.SHA256}}{
  data,err:=os.ReadFile(entry.path)
  if err!=nil{return "","",err}
  sum:=sha256.Sum256(data)
  if len(entry.expected)!=64||hex.EncodeToString(sum[:])!=entry.expected{return "","",fmt.Errorf("video tool integrity mismatch: %s",filepath.Base(entry.path))}
 }
 if err:=requireWebPEncoder(ctx,ffmpeg);err!=nil{return "","",err}
 first,err:=toolVersion(ctx,ffmpeg,"ffmpeg");if err!=nil{return "","",err}
 second,err:=toolVersion(ctx,ffprobe,"ffprobe");if err!=nil{return "","",err}
 if first!=manifest.FFmpeg.Version||second!=manifest.FFprobe.Version{return "","",fmt.Errorf("video tool executable version mismatch: %s / %s",first,second)}
 return ffmpeg,ffprobe,nil
}

func requireWebPEncoder(ctx context.Context,ffmpeg string) error {
 output,err:=exec.CommandContext(ctx,ffmpeg,"-hide_banner","-encoders").Output()
 if err!=nil{return fmt.Errorf("video: inspect WebP encoders: %w",err)}
 for _,line:=range strings.Split(string(output),"\n"){
  fields:=strings.Fields(line)
  if len(fields)>1 && fields[1]=="libwebp" && strings.HasPrefix(fields[0],"V"){return nil}
 }
 return errors.New("video: FFmpeg missing required libwebp encoder")
}

func resolveVideoTools(ctx context.Context) (string,string,error) {
 ffmpeg,ffprobe:=os.Getenv("SECRETARY_FFMPEG"),os.Getenv("SECRETARY_FFPROBE")
 if (ffmpeg=="")!=(ffprobe==""){return "","",errors.New("both SECRETARY_FFMPEG and SECRETARY_FFPROBE are required")}
 if ffmpeg==""{
  executable,err:=os.Executable();if err!=nil{return "","",err}
  release:=filepath.Dir(filepath.Dir(executable))
  if _,err=os.Stat(filepath.Join(release,"VERSION"));err==nil{return verifyManagedVideoTools(ctx,release)}
  ffmpeg,ffprobe="ffmpeg","ffprobe"
 }
 first,err:=toolVersion(ctx,ffmpeg,"ffmpeg");if err!=nil{return "","",err}
 second,err:=toolVersion(ctx,ffprobe,"ffprobe");if err!=nil{return "","",err}
 if first!=second{return "","",fmt.Errorf("incompatible FFmpeg/FFprobe versions: %s / %s",first,second)}
 if err:=requireWebPEncoder(ctx,ffmpeg);err!=nil{return "","",err}
 return ffmpeg,ffprobe,nil
}
