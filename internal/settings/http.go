package settings

import (
	"errors"
	"net/http"
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
	rt.Admin("GET /api/admin/settings", h.get)
	rt.Admin("PUT /api/admin/settings", h.put)
}

type response struct {
	CUIT              string    `json:"cuit"`
	LegalName         string    `json:"legalName"`
	Address           string    `json:"address"`
	GrossIncomeNumber string    `json:"grossIncomeNumber"`
	ActivityStartDate string    `json:"activityStartDate"`
	PointOfSale       int32     `json:"pointOfSale"`
	Category          Category  `json:"monotributoCategory"`
	AnnualCapCents    int64     `json:"annualCapCents"`
	ArcaEnv           ArcaEnv   `json:"arcaEnv"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func toResponse(s Settings) response {
	return response{
		CUIT:              s.CUIT,
		LegalName:         s.LegalName,
		Address:           s.Address,
		GrossIncomeNumber: s.GrossIncomeNumber,
		ActivityStartDate: s.ActivityStartDate.Format(dateLayout),
		PointOfSale:       s.PointOfSale,
		Category:          s.Category,
		AnnualCapCents:    int64(s.AnnualCap),
		ArcaEnv:           s.ArcaEnv,
		UpdatedAt:         s.UpdatedAt.UTC(),
	}
}

// updateRequest defers real validation to Service.Update, which needs the business date.
type updateRequest struct {
	Input
}

func (updateRequest) Validate() error { return nil }

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	s, err := h.svc.Get(r.Context())
	if errors.Is(err, ErrNotConfigured) {
		return httpx.JSON(w, http.StatusOK, (*response)(nil))
	}
	if err != nil {
		return err
	}
	resp := toResponse(s)
	return httpx.JSON(w, http.StatusOK, &resp)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) error {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		return httpx.ErrUnauthenticated
	}
	req, err := httpx.Decode[updateRequest](w, r)
	if err != nil {
		return err
	}
	s, err := h.svc.Update(r.Context(), audit.Actor{Type: audit.ActorAdmin, ID: p.UserID, IP: httpx.ClientIP(r)}, req.Input)
	if err != nil {
		return err
	}
	resp := toResponse(s)
	return httpx.JSON(w, http.StatusOK, &resp)
}
