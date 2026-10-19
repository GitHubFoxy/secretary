package traystatus

import (
 "encoding/json"
 "os"
 "strings"
 "time"

 "github.com/beruseruko/secretary/internal/core"
)

type Harness struct { Name string `json:"name"`; Ready bool `json:"ready"` }
type Snapshot struct {
 Connected bool `json:"connected"`
 ObservedAt time.Time `json:"observed_at"`
 Harnesses []Harness `json:"harnesses"`
}

func Write(path string, connected bool, inventory core.HarnessInventorySnapshot) error {
 snapshot:=Snapshot{Connected:connected,ObservedAt:time.Now().UTC()}
 for _,instance:=range inventory.Instances {
  snapshot.Harnesses=append(snapshot.Harnesses,Harness{Name:string(instance.Kind),Ready:instance.Available()})
 }
 data,err:=json.Marshal(snapshot)
 if err!=nil{return err}
 return os.WriteFile(path,data,0o600)
}

func Read(path string,running bool,now time.Time) Snapshot {
 if !running || path=="" {return Snapshot{}}
 data,err:=os.ReadFile(path)
 if err!=nil || len(data)>=64*1024{return Snapshot{}}
 var snapshot Snapshot
 if json.Unmarshal(data,&snapshot)!=nil || snapshot.ObservedAt.IsZero() || now.Before(snapshot.ObservedAt) || now.Sub(snapshot.ObservedAt)>=20*time.Second {return Snapshot{}}
 for _,h:=range snapshot.Harnesses {if strings.TrimSpace(h.Name)=="" {return Snapshot{}}}
 return snapshot
}
