package commands

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/CryptoLabInc/rune-console/internal/console"
	"github.com/CryptoLabInc/rune-console/internal/crypto"
	"github.com/CryptoLabInc/rune-console/internal/db"
	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
	"github.com/CryptoLabInc/rune-console/internal/server"
	"github.com/CryptoLabInc/rune-console/internal/storedb"
	"github.com/CryptoLabInc/rune-console/internal/storedb/yamlimport"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Manage the runeconsole daemon process",
		Hidden: true,
	}
	cmd.AddCommand(newDaemonStartCmd())
	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in the foreground",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStart(cmd.Context())
		},
	}
}

func runDaemonStart(ctx context.Context) error {
	cfg, err := server.LoadConfig(globals.configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Unified store database (runeconsole.db): opened unconditionally at boot
	// with the hardened posture (fail-closed 0600 perms, per-commit fsync,
	// integrity check) and schema v1 installed idempotently.
	storeDB, err := db.OpenStrict(cfg.StoreDBPath())
	if err != nil {
		return fmt.Errorf("daemon: open store db: %w", err)
	}
	defer func() { _ = storeDB.Close() }()
	if err := storedb.EnsureSchema(storeDB); err != nil {
		return fmt.Errorf("daemon: %w", err)
	}

	// One-time YAML→SQLite import, BEFORE any store is constructed: a first
	// boot over a legacy data directory moves all four YAML stores into the
	// database in one transaction and parks the files as *.yml.migrated; once
	// the schema_migrations version row exists, leftovers are warned about
	// and ignored; a fresh directory imports nothing (default roles are
	// seeded by the tokens store at load). The importer injects the same
	// person-key contract the daemon uses (members.ValidateID), so a legacy
	// email-keyed memberships.yml refuses to import just as it refused to
	// boot.
	groupsPath, membershipsPath := cfg.GroupsFiles()
	membersPath, invitesPath := cfg.MembersFiles()
	if err := yamlimport.Import(ctx, storeDB, yamlimport.Sources{
		RolesFile:       cfg.Tokens.RolesFile,
		TokensFile:      cfg.Tokens.TokensFile,
		MembersFile:     membersPath,
		InvitesFile:     invitesPath,
		GroupsFile:      groupsPath,
		MembershipsFile: membershipsPath,
	}, nil, slog.Default()); err != nil {
		return fmt.Errorf("daemon: yaml import: %w", err)
	}

	// Token + role store: loads from and writes through the unified store
	// database, seeding the default admin/member roles when absent.
	store := tokens.NewStore()
	if err := store.LoadFromDB(storeDB); err != nil {
		return fmt.Errorf("daemon: load tokens: %w", err)
	}
	defer store.Shutdown()

	// Group RBAC store (plan §6-D2): loads from and writes through the
	// unified store database; the judge stays pure in-memory reads. A cyclic
	// tree in the stored rows still fails startup on purpose.
	groupStore := groups.NewStore()
	groupStore.SetLimits(groups.Limits{
		TopKRead:  cfg.Groups.TopKRead,
		TopKWrite: cfg.Groups.TopKWrite,
	})
	// Organization admin(s) — the Owner identity that gates grant/revoke and
	// the org-wide member-roles listing (plan §5, §6-D8).
	groupStore.SetOrgAdmins(cfg.Groups.OrgAdmins...)
	// Member deployments key group memberships by the immutable member UUID,
	// not the email: inject the member-id contract before the store loads or
	// serves, so an email-keyed membership row refuses to boot (greenfield-
	// only, spec appendix B#10). ValidateID is a pure format check — no
	// ordering dependency on the member-store load below.
	groupStore.SetPersonKeyValidator(members.ValidateID)
	if err := groupStore.LoadFromDB(storeDB); err != nil {
		return fmt.Errorf("daemon: load groups: %w", err)
	}
	defer groupStore.Shutdown()

	// Member registry (design-decisions §6.6/§8.3): loads from and writes
	// through the unified store database.
	memberStore := members.NewStore()
	if err := memberStore.LoadFromDB(storeDB); err != nil {
		return fmt.Errorf("daemon: load members: %w", err)
	}
	defer memberStore.Shutdown()
	// One-time invite wrap store (design-decisions §8.3): loads from and
	// writes through the unified store database, sweeping aged-out pending
	// codes at boot.
	inviteStore := invites.NewStore()
	if err := inviteStore.LoadFromDB(storeDB); err != nil {
		return fmt.Errorf("daemon: load invites: %w", err)
	}
	defer inviteStore.Shutdown()
	mailer := server.NewLogMailer(cfg.MailLogFile())
	// The invite endpoint is what a remote rune-mcp dials, so it must be a
	// reachable address, not a loopback/bind host. When unset (or "auto") the
	// console advertises its auto-detected public IP (the TLS cert's SAN already
	// carries it); an explicit value is used verbatim.
	consoleEndpoint := cfg.Members.ConsoleEndpoint
	if consoleEndpoint == "" || consoleEndpoint == "auto" {
		if ip := server.DetectPublicIP(ctx); ip != "" {
			consoleEndpoint = net.JoinHostPort(ip, strconv.Itoa(cfg.Server.GRPC.Port))
			slog.Info("console: advertising auto-detected public endpoint for invites", "endpoint", consoleEndpoint)
		} else {
			consoleEndpoint = net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Server.GRPC.Port))
			slog.Warn("console: public IP detection failed; invite endpoint falls back to loopback", "endpoint", consoleEndpoint)
		}
	}
	inviteConn := server.InviteConnInfo{
		ConsoleEndpoint: consoleEndpoint,
		CAPemURL:        cfg.Members.CAPemURL,
		CAPemSHA256:     cfg.Members.CAPemSHA256,
	}

	keyParams := crypto.KeysParams{
		Root:  cfg.Keys.Path,
		KeyID: "rune-console-key",
		Dim:   cfg.Keys.EmbeddingDim,
	}
	if err := crypto.EnsureKeys(keyParams); err != nil {
		return fmt.Errorf("daemon: ensure keys: %w", err)
	}

	audit, err := server.NewAuditLogger(cfg.Audit)
	if err != nil {
		return err
	}
	defer audit.Close()

	// The runespace engine is attached lazily (nil here): until a data-plane
	// engine is connected, the gRPC ConsoleService reports "runespace not
	// configured". v.Close() releases the engine if one was connected.
	v := server.NewConsole(cfg, store, groupStore, nil, audit)
	defer v.Close()
	// Wire the dataplane member-status gate + judge-key resolver to the member
	// registry: a token whose user is a DISABLED member is denied on the gRPC
	// surface even if the token itself still validates (defense in depth for
	// member disable), and a registered user's token email resolves to the
	// member UUID the groups judge is keyed by.
	v.SetMemberDirectory(memberStore)
	// Wire the pre-auth invite redemption RPCs (LookupWrap/Unwrap — §8.3/§8.4
	// model P) to the invite wrap store and the member registry.
	v.SetInviteRedemption(inviteStore, memberStore)

	// Self-invite issuer for the console↔rune-mcp connection test: the BFF's
	// POST /api/v1/invite mints a fresh wrapped token for the operator and mails
	// the registration string via the cloud public API. selfInviteRole is the
	// token role bound to the invite (a plain teammate role — get_public_key +
	// decrypt scopes, enough to fetch the manifest and run insert/search).
	const selfInviteRole = "member"
	selfInviter := server.NewSelfInviteIssuer(v, memberStore, inviteStore, inviteConn, cfg.InviteTTL(), selfInviteRole)

	// Connect the data-plane engine eagerly only when a static runespace
	// endpoint is configured (dev/integration). Otherwise the connection is
	// deferred to the access-token-driven flow: open the full key set, dial
	// the runespace, register the eval key, and attach via ConnectEngine
	// (which also wires the group-delete sole-tag guard, plan §6-D7).
	if cfg.Runespace.Endpoint != "" {
		eng, oerr := crypto.OpenEngine(ctx, crypto.EngineParams{
			Keys:     keyParams,
			Endpoint: cfg.Runespace.Endpoint,
			Token:    cfg.Runespace.APIKey,
			Insecure: cfg.Runespace.Insecure,
		})
		if oerr != nil {
			return fmt.Errorf("daemon: open runespace engine: %w", oerr)
		}
		v.ConnectEngine(eng)
		slog.Info("console: runespace engine connected",
			"endpoint", cfg.Runespace.Endpoint, "insecure", cfg.Runespace.Insecure)
	} else {
		slog.Warn("console: runespace not configured — data-plane RPCs report 'runespace not configured' until connected")
	}

	slog.Info("console: starting daemon",
		"pid", os.Getpid(),
		"config", cfg.Source,
		"grpc_addr", fmt.Sprintf("%s:%d", cfg.Server.GRPC.Host, cfg.Server.GRPC.Port),
		"console_enabled", cfg.Server.Console.Enabled)

	// Admin operations handler (token/role/group + member/invite), mounted
	// cookie-gated under /admin/ on the console HTTP listener.
	adminHandler := server.NewAdminHandler(v, memberStore, inviteStore, mailer, inviteConn, cfg.InviteTTL())

	// Domain API handler (teams, users, memberships, invitations) — the design
	// doc's /api/v1 surface. Same RBAC stores as the admin handler; mounted
	// origin + session gated under /api/v1/ with the operator injected as actor.
	domainHandler := server.NewConsoleAPIHandler(v, memberStore, inviteStore, mailer, inviteConn, cfg.InviteTTL())

	// Console BFF HTTP handler (loopback auth + SPA + cookie-gated /api/v1 +
	// admin), built only when the console surface is enabled.
	var consoleHandler http.Handler
	if cfg.Server.Console.Enabled {
		sessDB, derr := db.Open(cfg.ConsoleDBPath())
		if derr != nil {
			return fmt.Errorf("daemon: open console session db: %w", derr)
		}
		defer func() { _ = sessDB.Close() }()
		var dp *console.Dataplane
		consoleHandler, dp, err = console.NewHandler(console.Deps{
			Port:              cfg.ConsolePort(),
			APIBaseURL:        cfg.Cloud.APIBaseURL,
			WebBaseURL:        cfg.Cloud.WebBaseURL,
			FrontendDir:       cfg.Server.Console.FrontendDir,
			DB:                sessDB,
			AdminHandler:      adminHandler,
			DomainHandler:     domainHandler,
			Connector:         v, // *server.Console: dials the runespace + attaches the engine
			Inviter:           selfInviter,
			RunespaceInsecure: cfg.Runespace.Insecure,
			Logger:            slog.Default(),
		})
		if err != nil {
			return fmt.Errorf("daemon: build console handler: %w", err)
		}
		if dp != nil {
			// Set the refresh-loop parent to the daemon lifetime and, if a
			// data-plane credential is already persisted, reconnect the engine
			// now so a restart resumes the data plane without a login.
			dp.Start(ctx)
		}
	}
	return server.Serve(ctx, v, consoleHandler)
}
