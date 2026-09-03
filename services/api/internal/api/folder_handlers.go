package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

type createFolderRequest struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}

	var body createFolderRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	v := validation.New()
	name := v.RequiredString("name", body.Name, meetings.MaxFolderName)
	var parentID *uuid.UUID
	if body.ParentID != "" {
		parsed := v.UUID("parent_id", body.ParentID)
		if parsed != uuid.Nil {
			parentID = &parsed
		}
	}
	if v.Write(w, r) {
		return
	}

	folder, err := s.meetings.CreateFolder(r.Context(), membership, name, parentID)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, newFolderView(folder))
}

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}

	folders, err := s.meetings.ListFolders(r.Context(), membership)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}

	view := folderListView{Folders: make([]folderView, 0, len(folders))}
	for _, folder := range folders {
		view.Folders = append(view.Folders, newFolderView(folder))
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

type updateFolderRequest struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
}

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}
	folderID, ok := pathUUID(r, folderParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That folder does not exist.")
		return
	}

	var body updateFolderRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	v := validation.New()
	var params meetings.UpdateFolderParams
	if body.Name != nil {
		name := v.RequiredString("name", *body.Name, meetings.MaxFolderName)
		params.Name = &name
	}
	if body.ParentID != nil {
		if *body.ParentID == "" {
			params.ClearParent = true
		} else {
			parsed := v.UUID("parent_id", *body.ParentID)
			if parsed != uuid.Nil {
				params.ParentID = &parsed
			}
		}
	}
	if v.Write(w, r) {
		return
	}

	folder, err := s.meetings.UpdateFolder(r.Context(), membership, folderID, params)
	if err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newFolderView(folder))
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	membership, ok := s.libraryActor(w, r)
	if !ok {
		return
	}
	folderID, ok := pathUUID(r, folderParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That folder does not exist.")
		return
	}

	if err := s.meetings.DeleteFolder(r.Context(), membership, folderID); err != nil {
		s.writeLibraryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
