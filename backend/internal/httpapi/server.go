package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/altasci/network-storage/backend/internal/authorization"
	"github.com/altasci/network-storage/backend/internal/oidc"
	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/altasci/network-storage/backend/internal/sharing"
	"github.com/altasci/network-storage/backend/internal/storage/factory"
	"github.com/go-chi/chi/v5"
)

const sessionCookie = "__Host-altasci_session"

// dummyPasswordHash keeps unknown, disabled, and passwordless account login
// attempts on the same Argon2id verification path as valid local accounts.
const dummyPasswordHash = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type Server struct {
	store                        *repository.Store
	authorization                *authorization.Service
	factory                      *factory.Factory
	box                          *security.SecretBox
	sharing                      *sharing.Service
	oidc                         *oidc.Service
	log                          *slog.Logger
	sessionIdle, sessionAbsolute time.Duration
	webURL, apiURL               string
}
type Options struct {
	Store          *repository.Store
	Authorization  *authorization.Service
	Factory        *factory.Factory
	Box            *security.SecretBox
	Sharing        *sharing.Service
	OIDC           *oidc.Service
	Log            *slog.Logger
	WebURL, APIURL string
}

func New(o Options) http.Handler {
	s := &Server{store: o.Store, authorization: o.Authorization, factory: o.Factory, box: o.Box, sharing: o.Sharing, oidc: o.OIDC, log: o.Log, sessionIdle: 24 * time.Hour, sessionAbsolute: 7 * 24 * time.Hour, webURL: o.WebURL, apiURL: o.APIURL}
	r := chi.NewRouter()
	r.Use(s.baseMiddleware, s.recoverer, s.cors)
	r.Get("/health/live", s.live)
	r.Get("/health/ready", s.ready)
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.login)
		r.Get("/auth/oidc/providers", s.oidcProviders)
		r.Get("/auth/oidc/{providerID}/start", s.oidcStart)
		r.Get("/auth/oidc/{providerID}/callback", s.oidcCallback)
		r.Route("/public/shares/{token}", func(r chi.Router) {
			r.Get("/", s.publicShare)
			r.Post("/verify", s.verifyShare)
			r.Get("/nodes", s.publicNodes)
			r.Post("/nodes/{nodeID}/download", s.publicDownload)
			r.Get("/nodes/{nodeID}/content", s.publicLocalContent)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Get("/auth/me", s.me)
			r.Get("/auth/csrf", s.csrfToken)
			r.Group(func(r chi.Router) {
				r.Use(s.csrf)
				r.Post("/auth/logout", s.logout)
				r.Post("/auth/change-password", s.changePassword)
				s.projectRoutes(r)
				s.nodeRoutes(r)
				s.shareRoutes(r)
				r.Group(func(r chi.Router) { r.Use(s.admin); s.adminRoutes(r) })
			})
		})
	})
	return r
}
func (s *Server) projectRoutes(r chi.Router) {
	r.Get("/storage-backends", s.listAvailableStorage)
	r.Get("/projects", s.listProjects)
	r.Post("/projects", s.createProject)
	r.Get("/projects/{projectID}", s.getProject)
	r.Patch("/projects/{projectID}", s.updateProject)
	r.Delete("/projects/{projectID}", s.deleteProject)
	r.Get("/projects/{projectID}/members", s.listMembers)
	r.Put("/projects/{projectID}/members/{userID}", s.putMember)
	r.Delete("/projects/{projectID}/members/{userID}", s.deleteMember)
	r.Get("/projects/{projectID}/nodes", s.listNodes)
	r.Post("/projects/{projectID}/directories", s.createDirectory)
	r.Post("/projects/{projectID}/uploads", s.createUpload)
}
func (s *Server) nodeRoutes(r chi.Router) {
	r.Get("/nodes/{nodeID}", s.getNode)
	r.Patch("/nodes/{nodeID}", s.updateNode)
	r.Delete("/nodes/{nodeID}", s.deleteNode)
	r.Post("/nodes/{nodeID}/download", s.download)
	r.Get("/nodes/{nodeID}/content", s.localContent)
	r.Head("/nodes/{nodeID}/content", s.localContent)
	r.Post("/uploads/{uploadID}/parts/presign", s.presignParts)
	r.Put("/uploads/{uploadID}/content", s.localUpload)
	r.Post("/uploads/{uploadID}/complete", s.completeUpload)
	r.Delete("/uploads/{uploadID}", s.abortUpload)
}
func (s *Server) shareRoutes(r chi.Router) {
	r.Get("/shares", s.listShares)
	r.Post("/nodes/{nodeID}/shares", s.createShare)
	r.Get("/shares/{shareID}", s.getShare)
	r.Patch("/shares/{shareID}", s.updateShare)
	r.Delete("/shares/{shareID}", s.deleteShare)
}
func (s *Server) adminRoutes(r chi.Router) {
	r.Get("/admin/users", s.listUsers)
	r.Post("/admin/users", s.createUser)
	r.Get("/admin/users/{userID}", s.getUser)
	r.Patch("/admin/users/{userID}", s.updateUser)
	r.Post("/admin/users/{userID}/reset-password", s.resetPassword)
	r.Post("/admin/users/{userID}/revoke-sessions", s.revokeSessions)
	r.Post("/admin/transfer", s.transferAdmin)
	r.Get("/admin/storage-backends", s.listStorage)
	r.Post("/admin/storage-backends", s.createStorage)
	r.Get("/admin/storage-backends/{backendID}", s.getStorage)
	r.Patch("/admin/storage-backends/{backendID}", s.updateStorage)
	r.Delete("/admin/storage-backends/{backendID}", s.deleteStorage)
	r.Post("/admin/storage-backends/{backendID}/test", s.testStorage)
	r.Get("/admin/settings", s.getSettings)
	r.Patch("/admin/settings", s.updateSettings)
	r.Get("/admin/oidc-providers", s.listOIDC)
	r.Post("/admin/oidc-providers", s.createOIDC)
	r.Get("/admin/oidc-providers/{providerID}", s.getOIDC)
	r.Patch("/admin/oidc-providers/{providerID}", s.updateOIDC)
	r.Delete("/admin/oidc-providers/{providerID}", s.deleteOIDC)
	r.Post("/admin/oidc-providers/{providerID}/test", s.testOIDC)
}
