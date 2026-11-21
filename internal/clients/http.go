package clients

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(rt *httpx.Router) {
	rt.Admin("GET /api/admin/clients", h.list)
	rt.Admin("POST /api/admin/clients", h.create)
	rt.Admin("GET /api/admin/clients/{id}", h.get)
	rt.Admin("PUT /api/admin/clients/{id}", h.update)
	rt.Admin("POST /api/admin/clients/{id}/archive", h.archive)
	rt.Admin("POST /api/admin/clients/{id}/unarchive", h.unarchive)
	rt.Admin("POST /api/admin/clients/{id}/contacts", h.createContact)
	rt.Admin("PUT /api/admin/clients/{id}/contacts/{contactId}", h.updateContact)
	rt.Admin("DELETE /api/admin/clients/{id}/contacts/{contactId}", h.deleteContact)
}

type summaryDTO struct {
	ID            string     `json:"id"`
	LegalName     string     `json:"legalName"`
	CUIT          string     `json:"cuit"`
	TaxCondition  string     `json:"taxCondition"`
	ArchivedAt    *time.Time `json:"archivedAt"`
	ContactsCount int        `json:"contactsCount"`
}

type listDTO struct {
	Items  []summaryDTO `json:"items"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

type contactDTO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	PortalAccess bool      `json:"portalAccess"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type detailDTO struct {
	ID           string       `json:"id"`
	LegalName    string       `json:"legalName"`
	CUIT         string       `json:"cuit"`
	TaxCondition string       `json:"taxCondition"`
	Address      string       `json:"address"`
	Notes        string       `json:"notes"`
	ArchivedAt   *time.Time   `json:"archivedAt"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	Contacts     []contactDTO `json:"contacts"`
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func toContactDTO(c Contact) contactDTO {
	return contactDTO{
		ID: c.ID, Name: c.Name, Email: c.Email, Role: string(c.Role), PortalAccess: c.PortalAccess,
		CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC(),
	}
}

func toDetailDTO(d Detail) detailDTO {
	contacts := make([]contactDTO, 0, len(d.Contacts))
	for _, c := range d.Contacts {
		contacts = append(contacts, toContactDTO(c))
	}
	return detailDTO{
		ID: d.ID, LegalName: d.LegalName, CUIT: d.CUIT, TaxCondition: string(d.TaxCondition),
		Address: d.Address, Notes: d.Notes, ArchivedAt: utcPtr(d.ArchivedAt),
		CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(), Contacts: contacts,
	}
}

// clientRequest and contactRequest defer real validation to the service.
type clientRequest struct {
	ClientInput
}

func (clientRequest) Validate() error { return nil }

type contactRequest struct {
	ContactInput
}

func (contactRequest) Validate() error { return nil }

func actorFrom(r *http.Request) (audit.Actor, error) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		return audit.Actor{}, httpx.ErrUnauthenticated
	}
	return audit.Actor{Type: audit.ActorAdmin, ID: p.UserID, IP: httpx.ClientIP(r)}, nil
}

func intParam(q url.Values, key string, errs httpx.FieldErrors) int {
	raw := q.Get(key)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		errs[key] = "invalid"
	}
	return n
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	errs := httpx.FieldErrors{}
	p := ListParams{Query: q.Get("q"), Limit: intParam(q, "limit", errs), Offset: intParam(q, "offset", errs)}
	switch q.Get("status") {
	case "", "active":
	case "archived":
		p.Archived = true
	default:
		errs["status"] = "invalid value"
	}
	if len(errs) > 0 {
		return errs
	}
	page, err := h.svc.List(r.Context(), p)
	if err != nil {
		return err
	}
	limit := p.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	items := make([]summaryDTO, 0, len(page.Items))
	for _, s := range page.Items {
		items = append(items, summaryDTO{
			ID: s.ID, LegalName: s.LegalName, CUIT: s.CUIT, TaxCondition: string(s.TaxCondition),
			ArchivedAt: utcPtr(s.ArchivedAt), ContactsCount: s.ContactsCount,
		})
	}
	return httpx.JSON(w, http.StatusOK, listDTO{Items: items, Total: page.Total, Limit: limit, Offset: p.Offset})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	actor, err := actorFrom(r)
	if err != nil {
		return err
	}
	req, err := decodeLimited[clientRequest](w, r)
	if err != nil {
		return err
	}
	d, err := h.svc.Create(r.Context(), actor, req.ClientInput)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, toDetailDTO(d))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	d, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, toDetailDTO(d))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	actor, err := actorFrom(r)
	if err != nil {
		return err
	}
	req, err := decodeLimited[clientRequest](w, r)
	if err != nil {
		return err
	}
	d, err := h.svc.Update(r.Context(), actor, r.PathValue("id"), req.ClientInput)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, toDetailDTO(d))
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) error {
	actor, err := actorFrom(r)
	if err != nil {
		return err
	}
	d, err := h.svc.Archive(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, toDetailDTO(d))
}

func (h *Handler) unarchive(w http.ResponseWriter, r *http.Request) error {
	actor, err := actorFrom(r)
	if err != nil {
		return err
	}
	d, err := h.svc.Unarchive(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, toDetailDTO(d))
}

func (h *Handler) createContact(w http.ResponseWriter, r *http.Request) error {
	actor, err := actorFrom(r)
	if err != nil {
		return err
	}
	req, err := decodeLimited[contactRequest](w, r)
	if err != nil {
		return err
	}
	c, err := h.svc.CreateContact(r.Context(), actor, r.PathValue("id"), req.ContactInput)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, toContactDTO(c))
}

func (h *Handler) updateContact(w http.ResponseWriter, r *http.Request) error {
	actor, err := actorFrom(r)
	if err != nil {
		return err
	}
	req, err := decodeLimited[contactRequest](w, r)
	if err != nil {
		return err
	}
	c, err := h.svc.UpdateContact(r.Context(), actor, r.PathValue("id"), r.PathValue("contactId"), req.ContactInput)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, toContactDTO(c))
}

func (h *Handler) deleteContact(w http.ResponseWriter, r *http.Request) error {
	actor, err := actorFrom(r)
	if err != nil {
		return err
	}
	if err := h.svc.DeleteContact(r.Context(), actor, r.PathValue("id"), r.PathValue("contactId")); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

const maxBody = 64 << 10

func decodeLimited[T httpx.Validator](w http.ResponseWriter, r *http.Request) (T, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	return httpx.Decode[T](w, r)
}
