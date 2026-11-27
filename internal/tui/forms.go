package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/yamldoc"
)

type serverFormValues = yamldoc.ServerValues
type appFormValues = yamldoc.AppValues

const (
	sourceKindKey     = "source-kind"
	sourceDirKey      = "source-dir"
	sourceGitURLKey   = "source-git-url"
	sourceBuildDirKey = "source-build-dir"
	sourceRepoKey     = "source-repo"
	sourceAssetKey    = "source-asset"
)

type formPurpose int

const (
	formAddServer formPurpose = iota
	formEditServer
	formServerSettings
	formAddApp
	formEditApp
	formEditAppSource
	formEditAppDomains
	formEditAppCommands
	formDeployVersion
	formRestorePath
	formLogsService
)

type formHost struct {
	purpose formPurpose
	title   string
	form    *huh.Form
	err     string
	// saving is set while a server write is in flight.
	saving bool
	// initialised is the form Init was last run for.
	initialised *huh.Form

	server *serverFormValues
	app    *appFormValues
	text   string

	serverName string
	appName    string
}

func formText(s string) string {
	return strings.TrimSpace(s)
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinCSV(items []string) string {
	return strings.Join(items, ", ")
}

func envLines(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	// ShipEnv sorts keys and ends with a newline.
	srv := &config.Server{Apps: map[string]*config.App{"x": {Env: env}}}
	return strings.TrimRight(srv.ShipEnv("x"), "\n")
}

func parseEnvLines(text string) (map[string]string, error) {
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("env: %q is not KEY=VALUE", line)
		}
		values[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	return values, nil
}

func newServerFormValues(srv *config.Server, isNew bool) *serverFormValues {
	v := &serverFormValues{IsNew: isNew, Connection: config.ConnectionRemote}
	if srv != nil {
		v.Name = srv.Name
		v.OriginalName = srv.Name
		v.Connection = srv.Connection
		if v.Connection == "" {
			v.Connection = config.ConnectionRemote
		}
		v.Host = srv.Host
		v.IdentityFile = srv.IdentityFile
		v.User = srv.User
	}
	return v
}

func serverRemoteHidden(v *serverFormValues) bool {
	return v.Connection == config.ConnectionLocal
}

func serverLocalHidden(v *serverFormValues) bool {
	return v.Connection != config.ConnectionLocal
}

func (m *model) newAppFormValues(server, app string, isNew bool) *appFormValues {
	v := &appFormValues{
		Server:            server,
		Name:              app,
		OriginalName:      app,
		IsNew:             isNew,
		SourceType:        "github-release",
		SourceKind:        "release",
		DeployMaintenance: true,
	}
	if isNew {
		return v
	}
	rec := m.recordFor(server, app)
	if rec == nil {
		return v
	}
	v.DomainsText = joinCSV(rec.Domains)
	v.RedirectsText = joinCSV(rec.Redirects)
	v.Upstream = rec.Upstream
	v.SourceType = rec.Source.Type
	v.SourceRepo = rec.Source.Repo
	v.SourceAsset = rec.Source.Asset
	v.SourceKind = "release"
	if rec.Source.Type == "image" {
		v.SourceKind = "dir"
		var localSource string
		var srv *config.Server
		if loaded, err := m.backend.Server(server); err == nil {
			srv = loaded
			if srv.Sources != nil {
				localSource = srv.Sources[app]
			}
		}
		localSource = firstNonEmpty(localSource, rec.Source.Path)
		if repo, ok := config.GitSource(localSource); ok {
			v.SourceKind = "git"
			v.SourceURL = repo.Transport
			if srv != nil {
				v.SourceBuildDir = srv.BuildDirs[app]
			}
		} else {
			v.SourcePath = localSource
		}
	}
	if rec.UID != nil {
		v.UID = strconv.Itoa(*rec.UID)
	}
	v.DataText = joinCSV(rec.Data)
	v.HealthPath = rec.Health.HTTPPath
	v.BackupSQLite = joinCSV(rec.Backup.SQLite)
	v.BackupFiles = joinCSV(rec.Backup.Files)
	if rec.Backup.Postgres != nil {
		v.PostgresService = rec.Backup.Postgres.Service
		v.PostgresDB = rec.Backup.Postgres.DB
		v.PostgresUser = rec.Backup.Postgres.User
	}
	v.Env = envLines(rec.Env)
	v.BuildCommands = rec.Build.Commands
	v.DeployCommands = rec.Deploy.Commands
	v.DeployMaintenance = rec.MaintenanceEnabled()
	return v
}

// localSourceFromForm returns the value to write to
// servers.<server>.sources.<app>, the relative build directory, and whether
// the entry should be kept.
func localSourceFromForm(v *appFormValues) (string, string, bool, error) {
	switch v.SourceKind {
	case "git":
		value := strings.TrimSpace(v.SourceURL)
		if value == "" {
			return "", "", false, fmt.Errorf("a Git repository URL is required")
		}
		source, err := config.ParseLocalSource(value)
		if err != nil {
			return "", "", false, err
		}
		if !source.IsGit {
			return "", "", false, fmt.Errorf("want a Git repository URL, not a directory")
		}
		buildDir, err := config.NormalizeBuildDir(v.SourceBuildDir)
		if err != nil {
			return "", "", false, fmt.Errorf("build directory: %w", err)
		}
		return value, buildDir, true, nil
	case "dir":
		value := strings.TrimSpace(v.SourcePath)
		if value == "" {
			return "", "", false, nil
		}
		source, err := config.ParseLocalSource(value)
		if err != nil {
			return "", "", false, err
		}
		if source.IsGit {
			return "", "", false, fmt.Errorf("want a source directory, not a Git repository URL")
		}
		return value, "", true, nil
	default:
		return "", "", false, nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// appFromForm builds a desired record from the form. source.path is deliberately
// omitted: it belongs only to this machine's local ship.yaml.
func appFromForm(v *appFormValues) (*config.App, error) {
	sourceType := strings.TrimSpace(v.SourceType)
	repo := strings.TrimSpace(v.SourceRepo)
	asset := strings.TrimSpace(v.SourceAsset)
	kind := v.SourceKind
	if kind == "" {
		kind = "release"
		if sourceType == "image" {
			kind = "dir"
		}
	}
	switch kind {
	case "release":
		sourceType = "github-release"
		v.BuildCommands = ""
	case "dir", "git":
		sourceType, repo, asset = "image", "", ""
	}
	app := &config.App{
		Domains:   splitCSV(v.DomainsText),
		Redirects: splitCSV(v.RedirectsText),
		Upstream:  strings.TrimSpace(v.Upstream),
		Source: config.Source{
			Type:  sourceType,
			Repo:  repo,
			Asset: asset,
		},
		Data:   splitCSV(v.DataText),
		Health: config.Health{HTTPPath: strings.TrimSpace(v.HealthPath)},
		Backup: config.BackupSpec{
			SQLite: splitCSV(v.BackupSQLite),
			Files:  splitCSV(v.BackupFiles),
		},
		Build:  config.BuildSpec{Commands: v.BuildCommands},
		Deploy: config.DeploySpec{Commands: v.DeployCommands, Maintenance: &v.DeployMaintenance},
	}
	if v.UID != "" {
		uid, err := strconv.Atoi(strings.TrimSpace(v.UID))
		if err != nil {
			return nil, fmt.Errorf("uid: %q is not a number", v.UID)
		}
		app.UID = &uid
	}
	if v.PostgresService != "" || v.PostgresDB != "" || v.PostgresUser != "" {
		app.Backup.Postgres = &config.PostgresSpec{
			Service: strings.TrimSpace(v.PostgresService),
			DB:      strings.TrimSpace(v.PostgresDB),
			User:    strings.TrimSpace(v.PostgresUser),
		}
	}
	env, err := parseEnvLines(v.Env)
	if err != nil {
		return nil, err
	}
	if len(env) > 0 {
		app.Env = env
	}
	return app, nil
}

func appRoutingFields(v *appFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Title("Domains (comma separated)").Value(&v.DomainsText),
		huh.NewInput().Title("Redirects (comma separated)").Value(&v.RedirectsText),
	}
}

// sourceSummary describes the source values loaded when the form opened. Blank
// optional values are called out as defaults so the first step is not a bare
// selector with no context.
func sourceSummary(v *appFormValues) string {
	switch v.SourceKind {
	case "git":
		url := strings.TrimSpace(v.SourceURL)
		if url == "" {
			url = "not set"
		}
		buildDir := strings.TrimSpace(v.SourceBuildDir)
		if buildDir == "" {
			buildDir = "repository root (default)"
		}
		return "Kind: Git repository URL\nGit URL: " + url + "\nBuild directory: " + buildDir
	case "release":
		repo := strings.TrimSpace(v.SourceRepo)
		if repo == "" {
			repo = "not set"
		}
		asset := strings.TrimSpace(v.SourceAsset)
		if asset == "" {
			if v.Name != "" {
				asset = v.Name + "_linux_{arch} (default)"
			} else {
				asset = "{app}_linux_{arch} (default)"
			}
		}
		return "Kind: GitHub release asset\nRepository: " + repo + "\nRelease asset: " + asset
	default:
		path := strings.TrimSpace(v.SourcePath)
		if path == "" {
			path = "not set (no local source directory)"
		}
		return "Kind: Source directory\nDirectory: " + path
	}
}

func appSourceKindField(v *appFormValues) huh.Field {
	return huh.NewSelect[string]().Title("Source").
		Options(
			huh.NewOption("Source directory", "dir"),
			huh.NewOption("Git repository URL", "git"),
			huh.NewOption("GitHub release asset", "release"),
		).
		Key(sourceKindKey).
		Value(&v.SourceKind)
}

func appSourceReleaseFields(v *appFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Title("Repository (owner/repo)").Key(sourceRepoKey).Value(&v.SourceRepo),
		huh.NewInput().Title("Release asset").Description("Default: {app}_linux_{arch}").Key(sourceAssetKey).Value(&v.SourceAsset),
	}
}

func appSourceImageFields(v *appFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Title("Source directory").
			Description("An absolute or ~/ path, stored only in this machine's ship.yaml, never on the server.").
			Key(sourceDirKey).
			Value(&v.SourcePath),
	}
}

func appSourceGitFields(v *appFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Title("Git repository URL").
			Description("https://, ssh:// or git@host:path. Stored only in this machine's ship.yaml, never on the server.").
			Key(sourceGitURLKey).
			Value(&v.SourceURL),
		huh.NewInput().Title("Build directory (optional)").
			Description("A relative directory inside the checkout, e.g. docs. Blank uses the repository root.").
			Key(sourceBuildDirKey).
			Value(&v.SourceBuildDir),
	}
}

func appBuildFields(v *appFormValues) []huh.Field {
	return []huh.Field{
		huh.NewText().Title("Build commands").
			Description("Run locally before docker build (image apps only).").
			Lines(5).ShowLineNumbers(true).Value(&v.BuildCommands),
	}
}

func appDeployFields(v *appFormValues) []huh.Field {
	return []huh.Field{
		huh.NewText().Title("Deploy commands").
			Description("Run on the server in the new image just before it goes live, e.g. php artisan migrate --force").
			Lines(5).ShowLineNumbers(true).Value(&v.DeployCommands),
		huh.NewConfirm().Title("Maintenance page while deploying").
			Description("Serve a maintenance page while the new image starts.").
			Affirmative("Yes").Negative("No").Value(&v.DeployMaintenance),
	}
}

func (m *model) buildForm(f *formHost) *huh.Form {
	width := m.formWidth()
	switch f.purpose {
	case formAddServer, formEditServer:
		v := f.server
		f.title = "Server"
		remoteFields := huh.NewGroup(
			huh.NewInput().Title("Host (user@host)").Value(&v.Host),
			huh.NewInput().Title("SSH private key (optional)").
				Description("Path to the key for this server, e.g. ~/.ssh/id_ed25519. Empty uses your ssh config and agent.").
				Value(&v.IdentityFile),
		).WithHideFunc(func() bool { return serverRemoteHidden(v) })
		localFields := huh.NewGroup(
			huh.NewInput().Title("Admin user (optional)").
				Description("The user to act as on this machine, e.g. ops. Empty uses the current user; ship uses sudo to act as root.").
				Value(&v.User),
		).WithHideFunc(func() bool { return serverLocalHidden(v) })
		return huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("Name").Value(&v.Name),
				huh.NewSelect[string]().Title("Connection").
					Options(
						huh.NewOption("Remote (SSH)", config.ConnectionRemote),
						huh.NewOption("Local (this server)", config.ConnectionLocal),
					).
					Value(&v.Connection),
			),
			remoteFields,
			localFields,
		).WithWidth(width)
	case formServerSettings:
		v := f.server
		f.title = "Server settings · " + f.serverName
		return huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Admin user").Value(&v.AdminUser),
			huh.NewInput().Title("Caddy image").Value(&v.CaddyImage),
		)).WithWidth(width)
	case formAddApp, formEditApp:
		v := f.app
		f.title = "Application"
		nameTitle := "Name"
		nameDescription := ""
		if !v.IsNew {
			nameDescription = "Names cannot be changed; create a new app and delete the old one."
		}
		basicsFields := []huh.Field{huh.NewInput().Title(nameTitle).Description(nameDescription).Value(&v.Name)}
		basicsFields = append(basicsFields, appRoutingFields(v)...)
		basicsFields = append(basicsFields, huh.NewInput().Title("Upstream (service:port)").Value(&v.Upstream))
		basics := huh.NewGroup(basicsFields...)
		storage := huh.NewGroup(
			huh.NewInput().Title("UID (optional)").Value(&v.UID),
			huh.NewInput().Title("Data directories (comma separated)").Value(&v.DataText),
			huh.NewInput().Title("Health path (optional)").Value(&v.HealthPath),
			huh.NewText().Title("Environment").Description("One KEY=VALUE per line; no secrets here.").Lines(4).Value(&v.Env),
		)
		source := huh.NewGroup(appSourceKindField(v))
		release := huh.NewGroup(appSourceReleaseFields(v)...).
			WithHideFunc(func() bool { return v.SourceKind != "release" })
		imageFields := appSourceImageFields(v)
		imageFields = append(imageFields, appBuildFields(v)...)
		image := huh.NewGroup(imageFields...).
			WithHideFunc(func() bool { return v.SourceKind != "dir" })
		gitFields := appSourceGitFields(v)
		gitFields = append(gitFields, appBuildFields(v)...)
		gitSource := huh.NewGroup(gitFields...).
			WithHideFunc(func() bool { return v.SourceKind != "git" })
		backup := huh.NewGroup(
			huh.NewInput().Title("SQLite backups (comma separated)").Value(&v.BackupSQLite),
			huh.NewInput().Title("File backups (comma separated)").Value(&v.BackupFiles),
			huh.NewInput().Title("Postgres service (optional)").Value(&v.PostgresService),
			huh.NewInput().Title("Postgres database").Value(&v.PostgresDB),
			huh.NewInput().Title("Postgres user").Value(&v.PostgresUser),
		)
		deploy := huh.NewGroup(appDeployFields(v)...)
		return huh.NewForm(basics, storage, source, release, image, gitSource, backup, deploy).WithWidth(width)
	case formEditAppSource, formEditAppDomains, formEditAppCommands:
		v := f.app
		f.title = "Edit " + v.Name
		switch f.purpose {
		case formEditAppSource:
			f.title = "Source · " + v.Name
			kind := huh.NewGroup(
				huh.NewNote().Title("Saved source").Description(sourceSummary(v)),
				appSourceKindField(v),
			)
			release := huh.NewGroup(appSourceReleaseFields(v)...).
				WithHideFunc(func() bool { return v.SourceKind != "release" })
			dir := huh.NewGroup(appSourceImageFields(v)...).
				WithHideFunc(func() bool { return v.SourceKind != "dir" })
			gitSource := huh.NewGroup(appSourceGitFields(v)...).
				WithHideFunc(func() bool { return v.SourceKind != "git" })
			return huh.NewForm(kind, release, dir, gitSource).WithWidth(width)
		case formEditAppDomains:
			f.title = "Domains & redirects · " + v.Name
			return huh.NewForm(huh.NewGroup(appRoutingFields(v)...)).WithWidth(width)
		default:
			f.title = "Build & deploy · " + v.Name
			fields := []huh.Field{}
			if v.SourceType == "image" {
				fields = append(fields, appBuildFields(v)...)
			}
			fields = append(fields, appDeployFields(v)...)
			return huh.NewForm(huh.NewGroup(fields...)).WithWidth(width)
		}
	case formDeployVersion, formRestorePath, formLogsService:
		input := huh.NewInput().Value(&f.text)
		switch f.purpose {
		case formDeployVersion:
			f.title = "Deploy version"
			if rec := m.recordFor(f.serverName, f.appName); rec != nil && rec.Source.Type == "image" {
				input.Title("Version").Description("Blank builds the current git tree; otherwise an already loaded tag.")
			} else {
				input.Title("Release tag").Description("Blank deploys the latest release, e.g. v1.2.3.")
			}
		case formRestorePath:
			f.title = "Restore from local path"
			input.Title("Archive path").Description("A local .tar.gz; it is uploaded before the restore.")
		case formLogsService:
			f.title = "Logs"
			input.Title("Service (blank = all services)")
		}
		return huh.NewForm(huh.NewGroup(input)).WithWidth(width)
	}
	return nil
}

func (f *formHost) submitValues() {
	if f.app == nil {
		return
	}
	f.app.Domains = splitCSV(f.app.DomainsText)
	f.app.Redirects = splitCSV(f.app.RedirectsText)
	f.app.Data = splitCSV(f.app.DataText)
	f.app.BackupSQLiteV = splitCSV(f.app.BackupSQLite)
	f.app.BackupFilesV = splitCSV(f.app.BackupFiles)
}
