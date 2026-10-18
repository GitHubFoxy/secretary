package node

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

var artifactReference = regexp.MustCompile(`artifact://art_[a-f0-9]{32}\b`)
var artifactIDPattern = regexp.MustCompile(`^art_[a-f0-9]{32}$`)

type ArtifactPayload struct {
	ID     string
	Name   string
	SHA256 string
	Data   []byte
	VideoStatus string
	VideoError string
	VideoFrames []core.VideoFrame
	CheckVideoStatus func(context.Context) (core.Artifact,error)
}

type ArtifactFetcher func(context.Context, string) (ArtifactPayload, error)

// ErrTransientArtifactStatus distinguishes server restarts, overload and
// transport interruptions from a durable video processing_status=failed.
var ErrTransientArtifactStatus = errors.New("node artifact: transient metadata status failure")

func ArtifactIDs(text string) []string {
	seen := make(map[string]bool)
	ids := make([]string, 0, 17)
	for len(text) > 0 {
		bounds := artifactReference.FindStringIndex(text)
		if bounds == nil {
			break
		}
		id := strings.TrimPrefix(text[bounds[0]:bounds[1]], "artifact://")
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
			if len(ids) > 16 {
				break
			}
		}
		text = text[bounds[1]:]
	}
	return ids
}

func ValidArtifactID(id string) bool { return artifactIDPattern.MatchString(id) }

func (a Authenticator) SignArtifactRequest(nodeRef core.NodeReference, artifactID string, epoch int64) string {
	mac := hmac.New(sha256.New, a.secret)
	_, _ = fmt.Fprintf(mac, "artifact-get\n%s\n%s\n%d", nodeRef, artifactID, epoch)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a Authenticator) VerifyArtifactRequest(nodeRef core.NodeReference, artifactID string, epoch int64, signature string, now time.Time) error {
	if !ValidArtifactID(artifactID) || epoch < now.Unix()-120 || epoch > now.Unix()+120 || len(a.secret) < 32 {
		return errors.New("node artifact: invalid request")
	}
	provided, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(provided, mustDecodeArtifactSignature(a.SignArtifactRequest(nodeRef, artifactID, epoch))) {
		return errors.New("node artifact: invalid authorization")
	}
	return nil
}

func mustDecodeArtifactSignature(value string) []byte {
	decoded, _ := hex.DecodeString(value)
	return decoded
}

// FetchArtifact tolerates transient Node-to-Server transport failures during
// original/derivative transfer. Authentication, 4xx, size, checksum and
// schema failures remain terminal. Every retry signs a fresh GET request.
func (i NodeIdentity) FetchArtifact(ctx context.Context,id string) (ArtifactPayload,error) {
	var last error
	for attempt:=0;attempt<5;attempt++ {
		if err:=ctx.Err();err!=nil{return ArtifactPayload{},err}
		if attempt>0 {
			delay:=time.Duration(150*(1<<(attempt-1)))*time.Millisecond
			select {case <-ctx.Done():return ArtifactPayload{},ctx.Err();case <-time.After(delay):}
		}
		payload,err:=i.fetchArtifactOnce(ctx,id)
		if err==nil{return payload,nil}
		if !errors.Is(err,ErrTransientArtifactStatus){return ArtifactPayload{},err}
		last=err
	}
	return ArtifactPayload{},fmt.Errorf("node artifact: temporary transfer unavailable after retries: %w",last)
}
func (i NodeIdentity) fetchArtifactOnce(ctx context.Context, id string) (ArtifactPayload, error) {
	if !ValidArtifactID(id) {
		return ArtifactPayload{}, errors.New("node artifact: invalid identifier")
	}
	auth, err := i.Authenticator()
	if err != nil {
		return ArtifactPayload{}, err
	}
	u, err := url.Parse(i.ConnectURL)
	if err != nil || !strings.HasSuffix(u.Path, "/v1/nodes/connect") {
		return ArtifactPayload{}, errors.New("node artifact: invalid server endpoint")
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path = strings.TrimSuffix(u.Path, "/v1/nodes/connect") + "/v1/artifacts/" + id
	u.RawQuery, u.Fragment = "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ArtifactPayload{}, err
	}
	now := time.Now().Unix()
	req.Header.Set("X-Secretary-Node", string(i.Node))
	req.Header.Set("X-Artifact-Timestamp", strconv.FormatInt(now, 10))
	req.Header.Set("X-Artifact-Signature", auth.SignArtifactRequest(i.Node, id, now))
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err()!=nil{return ArtifactPayload{},ctx.Err()}
		return ArtifactPayload{},fmt.Errorf("%w: transfer network failure: %v",ErrTransientArtifactStatus,err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode==http.StatusRequestTimeout || response.StatusCode==http.StatusTooManyRequests || response.StatusCode>=500 {
			return ArtifactPayload{},fmt.Errorf("%w: GET HTTP %d",ErrTransientArtifactStatus,response.StatusCode)
		}
		return ArtifactPayload{},fmt.Errorf("node artifact: HTTP %d",response.StatusCode)
	}
	if response.ContentLength > core.MaxArtifactBytes {
		return ArtifactPayload{}, errors.New("node artifact: response exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, core.MaxArtifactBytes+1))
	if err!=nil {
		if ctx.Err()!=nil{return ArtifactPayload{},ctx.Err()}
		return ArtifactPayload{},fmt.Errorf("%w: GET response interrupted: %v",ErrTransientArtifactStatus,err)
	}
	if len(data) == 0 || len(data) > core.MaxArtifactBytes {
		return ArtifactPayload{}, errors.New("node artifact: invalid byte count")
	}
	checksum := sha256.Sum256(data)
	expected := response.Header.Get("X-Artifact-SHA256")
	if expected == "" || !hmac.Equal([]byte(expected), []byte(hex.EncodeToString(checksum[:]))) {
		return ArtifactPayload{}, errors.New("node artifact: checksum mismatch")
	}
	name, err := base64.RawURLEncoding.DecodeString(response.Header.Get("X-Artifact-Name"))
	if err != nil || len(name) == 0 || len(name) > 180 {
		return ArtifactPayload{}, errors.New("node artifact: invalid filename metadata")
	}
	
	payload:=ArtifactPayload{ID:id,Name:string(name),SHA256:expected,Data:data,VideoStatus:response.Header.Get("X-Artifact-Video-Status"),VideoError:response.Header.Get("X-Artifact-Video-Error")}
	if payload.VideoStatus=="ready" {
		encoded:=response.Header.Get("X-Artifact-Video-Frames")
		decoded,decodeErr:=base64.RawURLEncoding.DecodeString(encoded)
		if decodeErr!=nil || len(decoded)==0 || json.Unmarshal(decoded,&payload.VideoFrames)!=nil || len(payload.VideoFrames)==0 || len(payload.VideoFrames)>16 {
			return ArtifactPayload{}, errors.New("node artifact: missing or invalid video frame metadata")
		}
	}
	payload.CheckVideoStatus=func(c context.Context)(core.Artifact,error){return i.FetchArtifactStatus(c,id)}
	return payload,nil
}

// FetchArtifactStatus polls only signed metadata; never downloads the original video BLOB.
func (i NodeIdentity) FetchArtifactStatus(ctx context.Context, id string) (core.Artifact,error) {
	if !ValidArtifactID(id) {return core.Artifact{},errors.New("node artifact: invalid identifier")}
	auth,err:=i.Authenticator()
	if err!=nil{return core.Artifact{},err}
	u,err:=url.Parse(i.ConnectURL)
	if err!=nil || !strings.HasSuffix(u.Path,"/v1/nodes/connect") {return core.Artifact{},errors.New("node artifact: invalid server endpoint")}
	switch u.Scheme {case "wss":u.Scheme="https";case "ws":u.Scheme="http";default:return core.Artifact{},errors.New("node artifact: invalid connect URL scheme")}
	u.Path=strings.TrimSuffix(u.Path,"/v1/nodes/connect")+"/v1/artifacts/"+id+"/status"
	u.RawQuery,u.Fragment="",""
	req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil)
	if err!=nil{return core.Artifact{},err}
	now:=time.Now().Unix()
	req.Header.Set("X-Secretary-Node",string(i.Node))
	req.Header.Set("X-Artifact-Timestamp",strconv.FormatInt(now,10))
	req.Header.Set("X-Artifact-Signature",auth.SignArtifactRequest(i.Node,id,now))
	response,err:=(&http.Client{Timeout:10*time.Second}).Do(req)
	if err!=nil{
		if ctx.Err()!=nil{return core.Artifact{},ctx.Err()}
		return core.Artifact{},fmt.Errorf("%w: request interrupted: %v",ErrTransientArtifactStatus,err)
	}
	defer response.Body.Close()
	if response.StatusCode!=http.StatusOK{
		if response.StatusCode==http.StatusRequestTimeout || response.StatusCode==http.StatusTooManyRequests || response.StatusCode>=500 {
			return core.Artifact{},fmt.Errorf("%w: HTTP %d",ErrTransientArtifactStatus,response.StatusCode)
		}
		return core.Artifact{},fmt.Errorf("video status: HTTP %d",response.StatusCode)
	}
	var metadata core.Artifact
	if err=json.NewDecoder(io.LimitReader(response.Body,16<<10)).Decode(&metadata);err!=nil{
		if ctx.Err()!=nil{return core.Artifact{},ctx.Err()}
		return core.Artifact{},fmt.Errorf("%w: interrupted metadata response: %v",ErrTransientArtifactStatus,err)
	}
	if metadata.ID!=id {return core.Artifact{},errors.New("video status: mismatched Artifact identifier")}
	return metadata,nil
}
