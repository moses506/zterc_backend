package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"zetrc-backend/internal/httpx"
	"zetrc-backend/internal/models"
	"zetrc-backend/internal/notify"
)

// CRUDHandler implements the generic Frappe-style endpoints the ZETRC
// frontend calls: post-data, get-data, patch-data, delete-doc, submit-doc and
// cancel-doc, all keyed by a "model" name rather than per-resource routes.
// It is mounted (with different access rules) under /api, /admin/api and
// /lecturer/api. Staff writes are audited and trigger notifications.
type CRUDHandler struct {
	db       *gorm.DB
	notifier *notify.Service
	ca       *CAService
}

func NewCRUDHandler(db *gorm.DB, notifier *notify.Service, ca *CAService) *CRUDHandler {
	return &CRUDHandler{db: db, notifier: notifier, ca: ca}
}

type crudRequest struct {
	Model string         `json:"model"`
	Body  map[string]any `json:"body"`
}

var identifierPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// sanitizeColumns drops any key that isn't a plain identifier, so values
// pulled from client-controlled JSON/headers can never be used to smuggle
// SQL through column names.
func sanitizeColumns(fields map[string]any) map[string]any {
	clean := make(map[string]any, len(fields))
	for k, v := range fields {
		if identifierPattern.MatchString(k) {
			clean[k] = v
		}
	}
	return clean
}

func modelFromRequest(r *http.Request) string {
	if m := r.URL.Query().Get("model"); m != "" {
		return m
	}
	return r.Header.Get("model")
}

// readCRUDRequest accepts two request shapes used by different parts of the
// frontend: the documented {"model": "X", "body": {...}} wrapper, and a
// flatter convention where the fields are the top-level JSON object and the
// model name travels in a "Model" header instead.
func (h *CRUDHandler) readCRUDRequest(r *http.Request) (crudRequest, error) {
	var raw map[string]any
	if err := httpx.DecodeJSON(r, &raw); err != nil {
		return crudRequest{}, err
	}

	model, _ := raw["model"].(string)
	if model == "" {
		model = r.Header.Get("Model")
	}

	if body, ok := raw["body"].(map[string]any); ok {
		return crudRequest{Model: model, Body: body}, nil
	}

	delete(raw, "model")
	return crudRequest{Model: model, Body: raw}, nil
}

// POST /…/post-data/
func (h *CRUDHandler) Create(w http.ResponseWriter, r *http.Request) {
	req, err := h.readCRUDRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	h.createRecord(w, r, req)
}

func (h *CRUDHandler) createRecord(w http.ResponseWriter, r *http.Request, req crudRequest) {
	if !models.IsRegistered(req.Model) {
		httpx.WriteError(w, http.StatusBadRequest, "Unknown model: "+req.Model)
		return
	}

	instance, _ := models.NewInstance(req.Model)
	raw, err := json.Marshal(sanitizeColumns(req.Body))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid record body.")
		return
	}
	if err := json.Unmarshal(raw, instance); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid record body.")
		return
	}

	h.beforeCreate(r, req.Model, instance)

	based := instance.(models.Based)
	if strings.TrimSpace(based.GetBase().Name) == "" {
		based.GetBase().Name = models.GenerateName(req.Model)
	}

	if err := h.db.Create(instance).Error; err != nil {
		httpx.WriteError(w, http.StatusConflict, "Could not create record: "+err.Error())
		return
	}

	syncAfterWrite(h.db, req.Model, instance)
	h.audit(r, "create", req.Model, based.GetBase().Name, req.Body)
	h.afterCreate(req.Model, instance)

	httpx.WriteData(w, http.StatusCreated, instance)
}

// GET /…/get-data/?model=X[&name=Y]
func (h *CRUDHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.getRecords(w, r, nil)
}

// scopeFunc narrows a query, e.g. to one lecturer's courses. nil = no scope.
type scopeFunc func(*gorm.DB) *gorm.DB

func (h *CRUDHandler) getRecords(w http.ResponseWriter, r *http.Request, scope scopeFunc) {
	model := modelFromRequest(r)
	if model == "" {
		httpx.WriteError(w, http.StatusBadRequest, "Missing model.")
		return
	}
	if !models.IsRegistered(model) {
		httpx.WriteError(w, http.StatusBadRequest, "Unknown model: "+model)
		return
	}

	if name := r.URL.Query().Get("name"); name != "" {
		instance, _ := models.NewInstance(model)
		query := h.db
		if scope != nil {
			query = scope(query)
		}
		if err := query.Where("name = ?", name).First(instance).Error; err != nil {
			httpx.WriteError(w, http.StatusNotFound, "Record not found.")
			return
		}
		httpx.WriteData(w, http.StatusOK, instance)
		return
	}

	slicePtr, _ := models.NewSlice(model)
	query := h.db
	if scope != nil {
		query = scope(query)
	}

	filtersRaw := r.URL.Query().Get("filters")
	if filtersRaw == "" {
		filtersRaw = r.Header.Get("filters")
	}
	if filtersRaw != "" {
		var filters map[string]any
		if err := json.Unmarshal([]byte(filtersRaw), &filters); err == nil {
			for column, value := range sanitizeColumns(filters) {
				query = query.Where(column+" = ?", value)
			}
		}
	}

	if err := query.Order("created_at DESC").Find(slicePtr).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not fetch records.")
		return
	}

	httpx.WriteData(w, http.StatusOK, slicePtr)
}

// PATCH /…/patch-data/
func (h *CRUDHandler) Update(w http.ResponseWriter, r *http.Request) {
	req, err := h.readCRUDRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	h.updateRecord(w, r, req)
}

func (h *CRUDHandler) updateRecord(w http.ResponseWriter, r *http.Request, req crudRequest) {
	if !models.IsRegistered(req.Model) {
		httpx.WriteError(w, http.StatusBadRequest, "Unknown model: "+req.Model)
		return
	}

	name, _ := req.Body["name"].(string)
	if name == "" {
		httpx.WriteError(w, http.StatusBadRequest, "body.name is required to identify the record.")
		return
	}

	instance, _ := models.NewInstance(req.Model)
	if err := h.db.Where("name = ?", name).First(instance).Error; err != nil {
		httpx.WriteError(w, http.StatusNotFound, "Record not found.")
		return
	}
	before := snapshot(instance)

	updates := sanitizeColumns(req.Body)
	delete(updates, "name")
	delete(updates, "docstatus") // docstatus changes only via submit-doc/cancel-doc

	if len(updates) > 0 {
		if err := h.db.Model(instance).Where("name = ?", name).Updates(updates).Error; err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "Could not update record: "+err.Error())
			return
		}
	}

	if err := h.db.Where("name = ?", name).First(instance).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not reload updated record.")
		return
	}

	syncAfterWrite(h.db, req.Model, instance)
	h.audit(r, "update", req.Model, name, changedFields(before, snapshot(instance)))
	h.afterUpdate(req.Model, before, instance)

	httpx.WriteData(w, http.StatusOK, instance)
}

// DELETE /…/delete-doc/
func (h *CRUDHandler) Delete(w http.ResponseWriter, r *http.Request) {
	req, err := h.readCRUDRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	h.deleteRecord(w, r, req)
}

func (h *CRUDHandler) deleteRecord(w http.ResponseWriter, r *http.Request, req crudRequest) {
	if !models.IsRegistered(req.Model) {
		httpx.WriteError(w, http.StatusBadRequest, "Unknown model: "+req.Model)
		return
	}

	name, _ := req.Body["name"].(string)
	if name == "" {
		httpx.WriteError(w, http.StatusBadRequest, "body.name is required to identify the record.")
		return
	}

	instance, _ := models.NewInstance(req.Model)
	if err := h.db.Where("name = ?", name).First(instance).Error; err != nil {
		httpx.WriteError(w, http.StatusNotFound, "Record not found.")
		return
	}
	before := snapshot(instance)

	if err := h.db.Where("name = ?", name).Delete(instance).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not delete record.")
		return
	}

	h.audit(r, "delete", req.Model, name, before)
	httpx.WriteData(w, http.StatusOK, map[string]any{"name": name, "deleted": true})
}

func (h *CRUDHandler) setDocstatus(w http.ResponseWriter, r *http.Request, status int) {
	req, err := h.readCRUDRequest(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if !models.IsRegistered(req.Model) {
		httpx.WriteError(w, http.StatusBadRequest, "Unknown model: "+req.Model)
		return
	}

	name, _ := req.Body["name"].(string)
	if name == "" {
		httpx.WriteError(w, http.StatusBadRequest, "body.name is required to identify the record.")
		return
	}

	instance, _ := models.NewInstance(req.Model)
	if err := h.db.Where("name = ?", name).First(instance).Error; err != nil {
		httpx.WriteError(w, http.StatusNotFound, "Record not found.")
		return
	}

	if err := h.db.Model(instance).Where("name = ?", name).Update("docstatus", status).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not update record status.")
		return
	}

	if err := h.db.Where("name = ?", name).First(instance).Error; err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not reload updated record.")
		return
	}

	h.audit(r, "update", req.Model, name, map[string]any{"docstatus": status})
	httpx.WriteData(w, http.StatusOK, instance)
}

// PUT /…/submit-doc/
func (h *CRUDHandler) Submit(w http.ResponseWriter, r *http.Request) {
	h.setDocstatus(w, r, models.DocstatusSubmitted)
}

// PUT /…/cancel-doc/
func (h *CRUDHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	h.setDocstatus(w, r, models.DocstatusCancelled)
}
