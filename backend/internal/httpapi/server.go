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
			r.With(s.sessionOnly).Get("/auth/me", s.me)
			r.With(s.sessionOnly).Get("/auth/csrf", s.csrfToken)
			r.Group(func(r chi.Router) {
				r.Use(s.csrf)
				r.With(s.sessionOnly).Post("/auth/logout", s.logout)
				r.With(s.sessionOnly).Post("/auth/change-password", s.changePassword)
				r.Group(func(r chi.Router) {
					r.Use(s.sessionOnly)
					r.Get("/api-keys", s.listAPIKeys)
					r.Post("/api-keys", s.createAPIKey)
					r.Put("/api-keys/{keyID}", s.updateAPIKey)
					r.Delete("/api-keys/{keyID}", s.revokeAPIKey)
				})
				s.projectRoutes(r)
				s.nodeRoutes(r)
				r.Group(func(r chi.Router) { r.Use(s.sessionOnly); s.shareRoutes(r) })
				r.Group(func(r chi.Router) { r.Use(s.sessionOnly, s.admin); s.adminRoutes(r) })
			})
		})
	})
	return r
}
func (s *Server) projectRoutes(r chi.Router) {
	r.With(s.apiAccess("projects:create")).Get("/storage-backends", s.listAvailableStorage)
	r.With(s.apiAccess("projects:read")).Get("/projects", s.listProjects)
	r.With(s.apiAccess("projects:create")).Post("/projects", s.createProject)
	r.With(s.apiAccess("projects:read")).Get("/projects/{projectID}", s.getProject)
	r.With(s.apiAccess("projects:write")).Patch("/projects/{projectID}", s.updateProject)
	r.With(s.apiAccess("projects:delete")).Delete("/projects/{projectID}", s.deleteProject)
	r.With(s.sessionOnly).Get("/projects/{projectID}/members", s.listMembers)
	r.With(s.sessionOnly).Put("/projects/{projectID}/members/{userID}", s.putMember)
	r.With(s.sessionOnly).Delete("/projects/{projectID}/members/{userID}", s.deleteMember)
	r.With(s.apiAccess("files:read")).Get("/projects/{projectID}/nodes", s.listNodes)
	r.With(s.apiAccess("files:write")).Post("/projects/{projectID}/directories", s.createDirectory)
	r.With(s.apiAccess("files:write")).Post("/projects/{projectID}/uploads", s.createUpload)
}
func (s *Server) nodeRoutes(r chi.Router) {
	r.With(s.apiAccess("files:read")).Get("/nodes/{nodeID}", s.getNode)
	r.With(s.apiAccess("files:write")).Patch("/nodes/{nodeID}", s.updateNode)
	r.With(s.apiAccess("files:delete")).Delete("/nodes/{nodeID}", s.deleteNode)
	r.With(s.apiAccess("files:read")).Post("/nodes/{nodeID}/download", s.download)
	r.With(s.apiAccess("files:read")).Get("/nodes/{nodeID}/content", s.localContent)
	r.With(s.apiAccess("files:read")).Head("/nodes/{nodeID}/content", s.localContent)
	r.With(s.apiAccess("files:write")).Post("/uploads/{uploadID}/parts/presign", s.presignParts)
	r.With(s.apiAccess("files:write")).Put("/uploads/{uploadID}/content", s.localUpload)
	r.With(s.apiAccess("files:write")).Post("/uploads/{uploadID}/complete", s.completeUpload)
	r.With(s.apiAccess("files:write")).Delete("/uploads/{uploadID}", s.abortUpload)
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
