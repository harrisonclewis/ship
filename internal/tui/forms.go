package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/harrisonclewis/ship/internal/config"
)

type formPurpose int

const (
	formAddServer formPurpose = iota
	formEditServer
	formAddApp
	formEditApp
	formDeployVersion
	formRestorePath
	formLogsService
)

type serverFormValues struct {
	Name         string
	Host         string
	IdentityFile string
	AdminUser    string
	Arch         string
	CaddyImage   string
	OriginalName string
	IsNew        bool
}

type appFormValues struct {
	Server            string
	Name              string
	OriginalName      string
	IsNew             bool
	DomainsText       string
	RedirectsText     string
	Upstream          string
	SourceType        string
	SourceRepo        string
	SourceAsset       string
	SourcePath        string
	UID               string
	DataText          string
	HealthPath        string
	BackupSQLite      string
	BackupFiles       string
	PostgresService   string
	PostgresDB        string
	PostgresUser      string
	Env               string
	BuildCommands     string
	DeployCommands    string
	DeployMaintenance bool

	Domains       []string
	Redirects     []string
	Data          []string
	BackupSQLiteV []string
	BackupFilesV  []string
}

type formHost struct {
	purpose formPurpose
	title   string
	form    *huh.Form
	err     string
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

func newServerFormValues(srv *config.Server, isNew bool) *serverFormValues {
	v := &serverFormValues{IsNew: isNew, Arch: config.DefaultArch, AdminUser: config.DefaultAdminUser, CaddyImage: config.DefaultCaddyImage}
	if srv != nil {
		v.Name = srv.Name
		v.OriginalName = srv.Name
		v.Host = srv.Host
		v.IdentityFile = srv.IdentityFile
		v.AdminUser = srv.AdminUser
		v.Arch = srv.Arch
		v.CaddyImage = srv.CaddyImage
	}
	return v
}

func (m *model) newAppFormValues(server, app string, isNew bool) *appFormValues {
	v := &appFormValues{
		Server:            server,
		Name:              app,
		OriginalName:      app,
		IsNew:             isNew,
		SourceType:        "github-release",
		DeployMaintenance: true,
	}
	if isNew {
		return v
	}
	srv, err := m.backend.Server(server)
	if err != nil {
		return v
	}
	def, err := srv.App(app)
	if err != nil {
		return v
	}
	v.DomainsText = joinCSV(def.Domains)
	v.RedirectsText = joinCSV(def.Redirects)
	v.Upstream = def.Upstream
	v.SourceType = def.Source.Type
	v.SourceRepo = def.Source.Repo
	asset, sourcePath := m.doc.appSourceRaw(server, app)
	v.SourceAsset = asset
	if sourcePath != "" {
		v.SourcePath = sourcePath
	} else {
		v.SourcePath = def.Source.Path
	}
	if def.UID != nil {
		v.UID = strconv.Itoa(*def.UID)
	}
	v.DataText = joinCSV(def.Data)
	v.HealthPath = def.Health.HTTPPath
	v.BackupSQLite = joinCSV(def.Backup.SQLite)
	v.BackupFiles = joinCSV(def.Backup.Files)
	if def.Backup.Postgres != nil {
		v.PostgresService = def.Backup.Postgres.Service
		v.PostgresDB = def.Backup.Postgres.DB
		v.PostgresUser = def.Backup.Postgres.User
	}
	v.Env = envLines(def.Env)
	v.BuildCommands, v.DeployCommands, v.DeployMaintenance = m.doc.appExtras(server, app)
	return v
}

func (m *model) buildForm(f *formHost) *huh.Form {
	width := m.formWidth()
	switch f.purpose {
	case formAddServer, formEditServer:
		v := f.server
		title := "Name"
		if !v.IsNew {
			title = "Name (rename)"
		}
		f.title = "Server"
		return huh.NewForm(huh.NewGroup(
			huh.NewInput().Title(title).Value(&v.Name),
			huh.NewInput().Title("Host (user@host)").Value(&v.Host),
			huh.NewInput().Title("SSH private key (optional)").
				Description("Path to the key for this server, e.g. ~/.ssh/id_ed25519. Empty uses your ssh config and agent.").
				Value(&v.IdentityFile),
			huh.NewInput().Title("Admin user").Value(&v.AdminUser),
			huh.NewSelect[string]().Title("Architecture").
				Options(huh.NewOption("linux/amd64", "linux/amd64"), huh.NewOption("linux/arm64", "linux/arm64")).
				Value(&v.Arch),
			huh.NewInput().Title("Caddy image").Value(&v.CaddyImage),
		)).WithWidth(width)
	case formAddApp, formEditApp:
		v := f.app
		f.title = "Application"
		nameTitle := "Name"
		if !v.IsNew {
			nameTitle = "Name (rename)"
		}
		basics := huh.NewGroup(
			huh.NewInput().Title(nameTitle).Value(&v.Name),
			huh.NewInput().Title("Domains (comma separated)").Value(&v.DomainsText),
			huh.NewInput().Title("Redirects (comma separated)").Value(&v.RedirectsText),
			huh.NewInput().Title("Upstream (service:port)").Value(&v.Upstream),
		)
		storage := huh.NewGroup(
			huh.NewInput().Title("UID (optional)").Value(&v.UID),
			huh.NewInput().Title("Data directories (comma separated)").Value(&v.DataText),
			huh.NewInput().Title("Health path (optional)").Value(&v.HealthPath),
			huh.NewText().Title("Environment").Description("One KEY=VALUE per line; no secrets here.").Lines(4).Value(&v.Env),
		)
		source := huh.NewGroup(
			huh.NewSelect[string]().Title("Source type").
				Options(huh.NewOption("GitHub release", "github-release"), huh.NewOption("Local image", "image")).
				Value(&v.SourceType),
		)
		release := huh.NewGroup(
			huh.NewInput().Title("Repository (owner/repo)").Value(&v.SourceRepo),
			huh.NewInput().Title("Release asset").Description("Default: {app}_linux_{arch}").Value(&v.SourceAsset),
		).WithHideFunc(func() bool { return v.SourceType != "github-release" })
		image := huh.NewGroup(
			huh.NewInput().Title("Local image source path").Value(&v.SourcePath),
			huh.NewText().Title("Build commands").
				Description("Run locally before docker build (image apps only).").
				Lines(5).ShowLineNumbers(true).Value(&v.BuildCommands),
		).WithHideFunc(func() bool { return v.SourceType != "image" })
		backup := huh.NewGroup(
			huh.NewInput().Title("SQLite backups (comma separated)").Value(&v.BackupSQLite),
			huh.NewInput().Title("File backups (comma separated)").Value(&v.BackupFiles),
			huh.NewInput().Title("Postgres service (optional)").Value(&v.PostgresService),
			huh.NewInput().Title("Postgres database").Value(&v.PostgresDB),
			huh.NewInput().Title("Postgres user").Value(&v.PostgresUser),
		)
		deploy := huh.NewGroup(
			huh.NewText().Title("Deploy commands").
				Description("Run on the server in the new image just before it goes live, e.g. php artisan migrate --force").
				Lines(5).ShowLineNumbers(true).Value(&v.DeployCommands),
			huh.NewConfirm().Title("Maintenance page while deploying").
				Description("Serve a maintenance page while the new image starts.").
				Affirmative("Yes").Negative("No").Value(&v.DeployMaintenance),
		)
		return huh.NewForm(basics, storage, source, release, image, backup, deploy).WithWidth(width)
	case formDeployVersion, formRestorePath, formLogsService:
		input := huh.NewInput().Value(&f.text)
		switch f.purpose {
		case formDeployVersion:
			f.title = "Deploy version"
			srv, _ := m.backend.Server(f.serverName)
			if srv != nil {
				if app, err := srv.App(f.appName); err == nil && app.Source.Type == "image" {
					input.Title("Version").Description("Blank builds the current git tree; otherwise an already loaded tag.")
				} else {
					input.Title("Release tag").Description("Blank deploys the latest release, e.g. v1.2.3.")
				}
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
