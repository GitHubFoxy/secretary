package webapi

import (
 "errors"
 "net/http"

 "github.com/beruseruko/secretary/internal/core"
 "github.com/beruseruko/secretary/internal/node"
)

func (s *Server) deleteArtifact(w http.ResponseWriter,r *http.Request){
 person,client,ok:=s.authorizedPerson(w,r)
 if !ok{return}
 if client!=nil&&!client.HasScope(core.ScopeConversationWrite){
  http.Error(w,"Client write scope required",http.StatusForbidden);return
 }
 id:=r.PathValue("id")
 if !node.ValidArtifactID(id){http.Error(w,"Invalid artifact reference",http.StatusBadRequest);return}
 err:=s.store.DeleteOwnerArtifact(r.Context(),person.ID,id)
 if errors.Is(err,core.ErrNotFound){http.Error(w,"Artifact not found",http.StatusNotFound);return}
 if err!=nil{http.Error(w,"Could not delete Artifact",http.StatusInternalServerError);return}
 w.WriteHeader(http.StatusNoContent)
}
