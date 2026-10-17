package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/telegram"
)

func TestT11TelegramImageSourceAuthValidationAndDurableIdempotency(t *testing.T) {
	ctx:=context.Background()
	store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"state.db"))
	if err!=nil {t.Fatal(err)}
	defer store.Close()
	api,err:=New(ctx,store,"bootstrap")
	if err!=nil {t.Fatal(err)}
	api.AttachInternalCredential("t11-private-credential")
	srv:=httptest.NewServer(api.Handler())
	defer srv.Close()
	image:=t10WebPNG(t)
	send:=func(token,name,mime string,data []byte)(int,map[string]any,string){
		t.Helper()
		req,err:=http.NewRequest(http.MethodPost,srv.URL+"/v1/internal/telegram/image/source",bytes.NewReader(data))
		if err!=nil {t.Fatal(err)}
		req.Header.Set("Authorization","Bearer "+token)
		req.Header.Set("X-Image-Update-ID","321")
		req.Header.Set("X-Image-Chat-ID","100")
		req.Header.Set("X-Image-Message-ID","22")
		req.Header.Set("X-Image-Name",name)
		req.Header.Set("X-Image-Mime-Type",mime)
		req.Header.Set("X-Image-Unique-ID","file-unique")
		res,err:=srv.Client().Do(req)
		if err!=nil {t.Fatal(err)}
		defer res.Body.Close()
		raw,err:=io.ReadAll(res.Body)
		if err!=nil {t.Fatal(err)}
		var obj map[string]any
		_ = json.Unmarshal(raw,&obj)
		return res.StatusCode,obj,string(raw)
	}
	if code,_,_:=send("wrong-token","picture.png","image/png",image);code!=http.StatusUnauthorized {t.Fatalf("unauthorized code=%d",code)}
	if code,_,_:=send("t11-private-credential","picture.png","image/jpeg",image);code!=http.StatusUnprocessableEntity {t.Fatalf("bad MIME=%d",code)}
	if code,_,_:=send("t11-private-credential","picture.png","image/png",[]byte("not a PNG"));code!=http.StatusUnprocessableEntity {t.Fatalf("invalid image accepted=%d",code)}
	if code,_,_:=send("t11-private-credential","picture.png","image/png",bytes.Repeat([]byte("a"),core.MaxArtifactBytes+1));code!=http.StatusRequestEntityTooLarge {t.Fatalf("oversized accepted=%d",code)}
	if code,_,_:=send("t11-private-credential","picture.svg","image/svg+xml",image);code!=http.StatusUnsupportedMediaType {t.Fatalf("unsupported accepted=%d",code)}
	code,first,raw:=send("t11-private-credential","picture.png","image/png",image)
	if code!=http.StatusCreated {t.Fatalf("source upload: %d %s",code,raw)}
	ref:=first["reference"].(string)
	if !strings.HasPrefix(ref,"artifact://art_") {t.Fatalf("missing ref: %#v",first)}
	code,second,raw:=send("t11-private-credential","picture.png","image/png",image)
	if code!=http.StatusOK || second["reference"]!=ref || second["duplicate"]!=true {t.Fatalf("replay: %d %#v %s",code,second,raw)}
	if code,_,_:=send("t11-private-credential","picture.png","image/png",t11OtherPNG(t));code!=http.StatusConflict {t.Fatalf("conflicting source accepted: %d",code)}
	artifact,body,err:=store.ArtifactForPerson(ctx,api.OwnerID(),strings.TrimPrefix(ref,"artifact://"))
	if err!=nil || !bytes.Equal(body,image) || artifact.MediaType!="image/png" || artifact.Name!="picture.png" {t.Fatalf("persisted artifact=%#v %v",artifact,err)}
	transport:=&telegram.HTTPServerClient{BaseURL:srv.URL,Credential:"t11-private-credential"}
	id,err:=transport.StoreImageOriginal(ctx,321,telegram.Message{ChatID:100,MessageID:22,Image:&telegram.Image{MimeType:"image/png",FileUniqueID:"file-unique"}},"picture.png",image)
	if err!=nil || id!=artifact.ID {t.Fatalf("production Telegram client retry: %s %v",id,err)}
}

func t11OtherPNG(t *testing.T) []byte {
	t.Helper()
	picture:=image.NewRGBA(image.Rect(0,0,2,2))
	picture.Set(1,1,color.RGBA{G:255,A:255})
	var output bytes.Buffer
	if err:=png.Encode(&output,picture);err!=nil {t.Fatal(err)}
	return output.Bytes()
}
