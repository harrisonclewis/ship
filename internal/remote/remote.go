// Package remote constructs and runs ssh, docker and local commands.
//
// Every argv is passed to exec directly (never through a shell). Remote
// commands are quoted for the remote shell and every caller validates its
// arguments first. A dry-run runner prints the argv instead of running it.
package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path"
	"strconv"
	"strings"
)

// AgentProtocol is the ship-agent protocol version embedded in this ship. The
// CLI refuses to replace a newer agent on a VPS.
const AgentProtocol = 6

const agentPath = "/usr/local/sbin/ship-agent"

// Cmd is one process invocation.
type Cmd struct {
	Path   string
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Context cancels the process. It is optional.
	Context context.Context
	// StdinSize is only used by the dry-run printer.
	StdinSize int
	// TTY marks interactive commands in dry-run output.
	TTY bool
	// Env, when set, is the complete environment for the command. A nil Env
	// inherits the current process environment.
	Env []string
}

// Runner runs commands. Tests and --dry-run provide their own.
type Runner interface {
	Run(Cmd) error
	Output(Cmd) ([]byte, error)
}

// ExecRunner runs real processes. Ctx is the default context for commands
// that do not carry their own.
type ExecRunner struct {
	Ctx context.Context
}

func (e ExecRunner) Run(c Cmd) error {
	ctx := c.Context
	if ctx == nil {
		ctx = e.Ctx
	}
	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, c.Path, c.Args...)
	} else {
		cmd = exec.Command(c.Path, c.Args...)
	}
	cmd.Stdin = c.Stdin
	cmd.Stdout = c.Stdout
	cmd.Stderr = c.Stderr
	if c.Env != nil {
		cmd.Env = c.Env
	}
	return cmd.Run()
}

func (e ExecRunner) Output(c Cmd) ([]byte, error) {
	var buf bytes.Buffer
	c.Stdout = &buf
	if err := e.Run(c); err != nil {
		return buf.Bytes(), err
	}
	return buf.Bytes(), nil
}

// DryRunner prints commands instead of running them.
type DryRunner struct {
	Out io.Writer
}

func (d DryRunner) Run(c Cmd) error {
	d.print(c)
	return nil
}

func (d DryRunner) Output(c Cmd) ([]byte, error) {
	d.print(c)
	return nil, nil
}

func (d DryRunner) print(c Cmd) {
	if d.Out == nil {
		return
	}
	fmt.Fprintf(d.Out, "$ %s\n", ShellJoin(append([]string{c.Path}, c.Args...)))
	if c.StdinSize > 0 {
		fmt.Fprintf(d.Out, "#   (stdin: %d bytes)\n", c.StdinSize)
	}
}

// Session is a connection to one VPS: an ssh connection, or the machine ship
// itself runs on when LocalMode is set.
type Session struct {
	Host string
	// IdentityFile is an optional private key passed to ssh with -i.
	IdentityFile string
	// LocalMode runs agent commands directly on this machine instead of over
	// ssh. Host and IdentityFile are unused.
	LocalMode bool
	// User is the local admin user to act as in LocalMode. Empty means the
	// current user.
	User   string
	DryRun bool
	Out    io.Writer
	Err    io.Writer
	Runner Runner
	Ctx    context.Context
}

// NewSession returns a session using the system ssh binary, or a printer in
// dry-run mode.
func NewSession(host string, dryRun bool, out, errOut io.Writer) *Session {
	return newSession(host, dryRun, out, errOut, nil)
}

// NewSessionContext is NewSession with a context that cancels its commands.
func NewSessionContext(host string, dryRun bool, out, errOut io.Writer, ctx context.Context) *Session {
	return newSession(host, dryRun, out, errOut, ctx)
}

// NewLocalSessionContext returns a session that runs commands on this machine.
func NewLocalSessionContext(dryRun bool, out, errOut io.Writer, ctx context.Context) *Session {
	s := newSession("", dryRun, out, errOut, ctx)
	s.LocalMode = true
	return s
}

func newSession(host string, dryRun bool, out, errOut io.Writer, ctx context.Context) *Session {
	r := Runner(ExecRunner{Ctx: ctx})
	if dryRun {
		r = DryRunner{Out: out}
	}
	return &Session{Host: host, DryRun: dryRun, Out: out, Err: errOut, Runner: r, Ctx: ctx}
}

// ShellQuote quotes one argument for a POSIX shell.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellJoin joins argv into a copy-pasteable shell line.
func ShellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = ShellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// SSHArgv builds the local ssh invocation for a remote shell command.
// identityFile, when set, is the only key ssh offers (IdentitiesOnly), so a
// full agent can't exhaust the server's auth attempts first.
func SSHArgv(host, identityFile, remoteCmd string, tty bool) []string {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15"}
	if identityFile != "" {
		args = append(args, "-i", identityFile, "-o", "IdentitiesOnly=yes")
	}
	if tty {
		args = append(args, "-t")
	}
	args = append(args, host, remoteCmd)
	return args
}

// AgentRemote builds the remote command that invokes the installed agent.
func AgentRemote(args ...string) string {
	quoted := make([]string, 0, len(args)+2)
	quoted = append(quoted, "sudo", "-n", "/usr/local/sbin/ship-agent")
	for _, a := range args {
		quoted = append(quoted, ShellQuote(a))
	}
	return strings.Join(quoted, " ")
}

// AgentArgv returns the full local ssh argv for an agent action.
func (s *Session) AgentArgv(tty bool, args ...string) []string {
	return SSHArgv(s.Host, s.IdentityFile, AgentRemote(args...), tty)
}

// geteuid and currentUser are variables so tests can pin the local identity.
var (
	geteuid     = os.Geteuid
	currentUser = func() string {
		if u, err := user.Current(); err == nil && u.Username != "" {
			return u.Username
		}
		return os.Getenv("USER")
	}
)

// argvWithPrefix combines a command prefix (sudo and options) with a program
// and its arguments. An empty prefix leaves the command unchanged.
func argvWithPrefix(prefix []string, program string, args []string) (string, []string) {
	if len(prefix) == 0 {
		return program, args
	}
	argv := make([]string, 0, len(prefix)-1+1+len(args))
	argv = append(argv, prefix[1:]...)
	argv = append(argv, program)
	argv = append(argv, args...)
	return prefix[0], argv
}

// localRootArgv returns the prefix that runs a command as root on this
// machine. It is empty when ship already runs as root. With a configured user
// different from the current one, ship first becomes that user and then takes
// the same sudo path to root.
func (s *Session) localRootArgv(tty bool) []string {
	if geteuid() == 0 {
		return nil
	}
	prefix := []string{"sudo"}
	if !tty {
		prefix = append(prefix, "-n")
	}
	if s.User != "" && s.User != currentUser() {
		prefix = append(prefix, "-u", s.User, "sudo")
		if !tty {
			prefix = append(prefix, "-n")
		}
	}
	return prefix
}

// localUserArgv returns the prefix that runs a command as the configured local
// admin user, for commands that elevate on their own (sudoedit).
func (s *Session) localUserArgv(tty bool) []string {
	if geteuid() == 0 || s.User == "" || s.User == currentUser() {
		return nil
	}
	prefix := []string{"sudo"}
	if !tty {
		prefix = append(prefix, "-n")
	}
	return append(prefix, "-u", s.User)
}

// AgentCommand returns the executable and arguments for an agent action.
// Remote sessions run ssh; local sessions run the agent directly with the
// local sudo rules.
func (s *Session) AgentCommand(tty bool, args ...string) (string, []string) {
	if s.LocalMode {
		return argvWithPrefix(s.localRootArgv(tty), agentPath, args)
	}
	return "ssh", SSHArgv(s.Host, s.IdentityFile, AgentRemote(args...), tty)
}

// RunAgent runs an agent action with inherited stdout/stderr.
func (s *Session) RunAgent(args ...string) error {
	program, argv := s.AgentCommand(false, args...)
	return s.Runner.Run(Cmd{
		Path:   program,
		Args:   argv,
		Stdout: s.Out,
		Stderr: s.Err,
	})
}

// RunAgentTTY runs an agent action with a TTY (log following, interactive).
func (s *Session) RunAgentTTY(args ...string) error {
	program, argv := s.AgentCommand(true, args...)
	return s.Runner.Run(Cmd{
		Path:   program,
		Args:   argv,
		Stdin:  os.Stdin,
		Stdout: s.Out,
		Stderr: s.Err,
		TTY:    true,
	})
}

// OutputAgent runs an agent action and captures stdout.
func (s *Session) OutputAgent(args ...string) ([]byte, error) {
	program, argv := s.AgentCommand(false, args...)
	return s.Runner.Output(Cmd{Path: program, Args: argv, Stderr: s.Err})
}

// StreamAgent runs an agent action and copies stdout to w without buffering
// it first. Callers cap the writer when the output comes from the server.
func (s *Session) StreamAgent(w io.Writer, args ...string) error {
	program, argv := s.AgentCommand(false, args...)
	return s.Runner.Run(Cmd{
		Path:   program,
		Args:   argv,
		Stdout: w,
		Stderr: s.Err,
	})
}

// InputAgent runs an agent action with bytes on stdin (never on the command
// line).
func (s *Session) InputAgent(stdin []byte, args ...string) error {
	program, argv := s.AgentCommand(false, args...)
	return s.Runner.Run(Cmd{
		Path:      program,
		Args:      argv,
		Stdin:     bytes.NewReader(stdin),
		Stdout:    s.Out,
		Stderr:    s.Err,
		StdinSize: len(stdin),
	})
}

// InputAgentOutput runs an agent action with bytes on stdin and captures its
// stdout, for commands that report the new configuration hash.
func (s *Session) InputAgentOutput(stdin []byte, args ...string) ([]byte, error) {
	program, argv := s.AgentCommand(false, args...)
	var buf bytes.Buffer
	err := s.Runner.Run(Cmd{
		Path:      program,
		Args:      argv,
		Stdin:     bytes.NewReader(stdin),
		Stdout:    &buf,
		Stderr:    s.Err,
		StdinSize: len(stdin),
	})
	return buf.Bytes(), err
}

// ShellCommand returns the executable and arguments that run script through a
// shell as root. Remote sessions use ssh with sudo; local sessions apply the
// local sudo rules (none when ship already runs as root).
func (s *Session) ShellCommand(script string, tty bool) (string, []string) {
	if s.LocalMode {
		return argvWithPrefix(s.localRootArgv(tty), "sh", []string{"-c", script})
	}
	sudo := "sudo -n"
	if tty {
		sudo = "sudo"
	}
	return "ssh", SSHArgv(s.Host, s.IdentityFile, sudo+" sh -c "+ShellQuote(script), tty)
}

// RunElevated runs a shell script as root with inherited output.
func (s *Session) RunElevated(script string, tty bool) error {
	program, argv := s.ShellCommand(script, tty)
	c := Cmd{Path: program, Args: argv, Stdout: s.Out, Stderr: s.Err, TTY: tty}
	if tty {
		c.Stdin = os.Stdin
	}
	return s.Runner.Run(c)
}

// OutputElevated runs a shell script as root and captures stdout.
func (s *Session) OutputElevated(script string) ([]byte, error) {
	program, argv := s.ShellCommand(script, false)
	return s.Runner.Output(Cmd{Path: program, Args: argv, Stderr: s.Err})
}

// StreamElevated runs a shell script as root and copies stdout to w.
func (s *Session) StreamElevated(script string, w io.Writer) error {
	program, argv := s.ShellCommand(script, false)
	return s.Runner.Run(Cmd{Path: program, Args: argv, Stdout: w, Stderr: s.Err})
}

// DownloadFile copies a root-owned server file to w.
func (s *Session) DownloadFile(filePath string, w io.Writer) error {
	return s.StreamElevated("cat "+ShellQuote(filePath), w)
}

// EnvEditCommand returns the executable and arguments that prepare envPath and
// open it in an editor with a TTY. Local sessions use sudoedit; remote
// sessions run sudo -e over ssh.
func (s *Session) EnvEditCommand(envPath string, tty bool) (string, []string) {
	if !s.LocalMode {
		dir := path.Dir(envPath)
		remoteCmd := fmt.Sprintf("sudo -n install -d -m 0755 %s; sudo -n test -f %s || sudo -n install -m 0600 /dev/null %s; sudo -n chmod 0600 %s; sudo -n -e %s",
			ShellQuote(dir), ShellQuote(envPath), ShellQuote(envPath), ShellQuote(envPath), ShellQuote(envPath))
		return "ssh", SSHArgv(s.Host, s.IdentityFile, remoteCmd, tty)
	}
	dir := path.Dir(envPath)
	root := s.localRootArgv(tty)
	user := s.localUserArgv(tty)
	line := func(prefix []string, program string, args ...string) string {
		exe, argv := argvWithPrefix(prefix, program, args)
		return ShellJoin(append([]string{exe}, argv...))
	}
	script := line(root, "install", "-d", "-m", "0755", dir) + "; " +
		line(root, "test", "-f", envPath) + " || " + line(root, "install", "-m", "0600", "/dev/null", envPath) + "; " +
		line(root, "chmod", "0600", envPath) + "; " +
		line(user, "sudoedit", envPath)
	return "sh", []string{"-c", script}
}

// RunEnvEdit prepares envPath and hands the terminal to the editor.
func (s *Session) RunEnvEdit(envPath string) error {
	program, argv := s.EnvEditCommand(envPath, true)
	return s.Runner.Run(Cmd{
		Path:   program,
		Args:   argv,
		Stdin:  os.Stdin,
		Stdout: s.Out,
		Stderr: s.Err,
		TTY:    true,
	})
}

// RunSSH runs a raw remote shell command with a TTY.
func (s *Session) RunSSH(remoteCmd string, tty bool) error {
	c := Cmd{
		Path:   "ssh",
		Args:   SSHArgv(s.Host, s.IdentityFile, remoteCmd, tty),
		Stdout: s.Out,
		Stderr: s.Err,
		TTY:    tty,
	}
	if tty {
		c.Stdin = os.Stdin
	}
	return s.Runner.Run(c)
}

// OutputSSH runs a raw remote command and captures stdout.
func (s *Session) OutputSSH(remoteCmd string) ([]byte, error) {
	return s.Runner.Output(Cmd{
		Path:   "ssh",
		Args:   SSHArgv(s.Host, s.IdentityFile, remoteCmd, false),
		Stderr: s.Err,
	})
}

// StreamSSH streams a remote command's stdout to w (used for downloads).
func (s *Session) StreamSSH(remoteCmd string, w io.Writer) error {
	return s.Runner.Run(Cmd{
		Path:   "ssh",
		Args:   SSHArgv(s.Host, s.IdentityFile, remoteCmd, false),
		Stdout: w,
		Stderr: s.Err,
	})
}

// UploadStream installs data at remotePath with the given mode by streaming
// it over ssh stdin. The content never appears in argv.
func (s *Session) UploadStream(remotePath string, mode fs.FileMode, r io.Reader, size int64) error {
	if !strings.HasPrefix(remotePath, "/") {
		return fmt.Errorf("upload: remote path must be absolute: %q", remotePath)
	}
	dir := path.Dir(remotePath)
	// mkdir -p leaves an existing directory's mode alone (notably /tmp).
	// umask 077 keeps the file 0600 from creation, not just after chmod. The
	// data goes to a temp file renamed into place, so a reader (bash running
	// ship-agent, a compose up) never sees a half-written file.
	script := `umask 077; t=$(mktemp "$1.XXXXXX") && cat > "$t" && chmod "$2" "$t" && mv -f "$t" "$1" || { rm -f "$t"; exit 1; }`
	if s.LocalMode {
		shell := "mkdir -p " + ShellQuote(dir) + " && sh -c " + ShellQuote(script) + " _ " +
			ShellQuote(remotePath) + " " + ShellQuote(fmt.Sprintf("%04o", mode.Perm()))
		program, argv := s.ShellCommand(shell, false)
		return s.Runner.Run(Cmd{
			Path:      program,
			Args:      argv,
			Stdin:     r,
			Stdout:    s.Out,
			Stderr:    s.Err,
			StdinSize: int(size),
		})
	}
	remote := fmt.Sprintf("sudo -n mkdir -p %s && sudo -n sh -c %s _ %s %s",
		ShellQuote(dir), ShellQuote(script), ShellQuote(remotePath),
		ShellQuote(fmt.Sprintf("%04o", mode.Perm())))
	return s.Runner.Run(Cmd{
		Path:      "ssh",
		Args:      SSHArgv(s.Host, s.IdentityFile, remote, false),
		Stdin:     r,
		Stdout:    s.Out,
		Stderr:    s.Err,
		StdinSize: int(size),
	})
}

// UploadFile installs a byte slice at remotePath.
func (s *Session) UploadFile(remotePath string, mode fs.FileMode, data []byte) error {
	return s.UploadStream(remotePath, mode, bytes.NewReader(data), int64(len(data)))
}

// agentProbe prints the installed agent's protocol version and sha256 in one
// round trip. A missing or pre-versioning script reports version 0.
const agentProbe = `f=/usr/local/sbin/ship-agent; if [ -f "$f" ]; then printf 'version='; sed -n 's/^AGENT_VERSION[[:space:]]*=[[:space:]]*//p' "$f" | head -n1; printf 'sha256='; sha256sum "$f" | cut -d' ' -f1; else printf 'version=0\nsha256=none\n'; fi`

// EnsureAgent installs ship-agent when it is missing, older, or has a
// different checksum at the same protocol version. A newer remote agent is
// never overwritten. The upload goes over stdin; the script is never in argv.
func (s *Session) EnsureAgent(agent []byte) error {
	sum := sha256.Sum256(agent)
	want := hex.EncodeToString(sum[:])
	if s.DryRun {
		fmt.Fprintf(s.Out, "# would check the protocol version and sha256 of %s, then install it if needed\n", agentPath)
		return nil
	}
	// Never replace a working agent with nothing (e.g. an Ops built without
	// the embedded script).
	if len(bytes.TrimSpace(agent)) == 0 {
		return fmt.Errorf("refusing to install ship-agent: the embedded agent script is empty")
	}
	out, err := s.OutputElevated(agentProbe)
	if err != nil {
		return fmt.Errorf("check ship-agent: %w", err)
	}
	version, got := parseAgentProbe(out)
	if version > AgentProtocol {
		return fmt.Errorf("the ship-agent on this server is newer (protocol %d) than this ship (%d); run `ship update`", version, AgentProtocol)
	}
	if version == AgentProtocol && got == want {
		return nil
	}
	fmt.Fprintln(s.Err, "ship: installing/updating ship-agent on this server")
	return s.UploadFile(agentPath, 0o755, agent)
}

func parseAgentProbe(out []byte) (int, string) {
	version := 0
	sum := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "version="); ok {
			version, _ = strconv.Atoi(strings.TrimSpace(v))
		} else if v, ok := strings.CutPrefix(line, "sha256="); ok {
			sum = strings.TrimSpace(v)
		}
	}
	return version, sum
}

// ShipImage streams a locally built image to an agent action as a
// gzip-compressed `docker save` tar on stdin. Compression happens in Go, so
// no local gzip binary is needed (notably on Windows).
func (s *Session) ShipImage(ref string, args ...string) error {
	if s.LocalMode {
		// The image was built into this machine's Docker daemon, which is the
		// server's daemon. image-load would resolve the same ref from
		// .ship-meta, so only verify it is there.
		if s.DryRun {
			fmt.Fprintf(s.Out, "$ %s\n", ShellJoin([]string{"docker", "image", "inspect", ref}))
			fmt.Fprintln(s.Out, "#   (local: image is already in this server's Docker daemon)")
			return nil
		}
		if err := s.Local("docker", "image", "inspect", ref); err != nil {
			return fmt.Errorf("verify local image %s: %w", ref, err)
		}
		return nil
	}
	argv := s.AgentArgv(false, args...)
	if s.DryRun {
		fmt.Fprintf(s.Out, "$ %s | gzip | %s\n",
			ShellJoin([]string{"docker", "save", ref}),
			ShellJoin(append([]string{"ssh"}, argv...)))
		fmt.Fprintln(s.Out, "#   (stream: docker save, gzip in Go, ssh stdin)")
		return nil
	}

	var save, sshCmd *exec.Cmd
	if s.Ctx != nil {
		save = exec.CommandContext(s.Ctx, "docker", "save", ref)
		sshCmd = exec.CommandContext(s.Ctx, "ssh", argv...)
	} else {
		save = exec.Command("docker", "save", ref)
		sshCmd = exec.Command("ssh", argv...)
	}
	save.Stderr = s.Err
	saveOut, err := save.StdoutPipe()
	if err != nil {
		return err
	}
	sshCmd.Stdout = s.Out
	sshCmd.Stderr = s.Err
	sshIn, err := sshCmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := sshCmd.Start(); err != nil {
		return fmt.Errorf("ssh: %w", err)
	}
	if err := save.Start(); err != nil {
		_ = sshIn.Close()
		_ = sshCmd.Wait()
		return fmt.Errorf("docker save: %w", err)
	}
	// Copy here rather than through io.Pipe: if ssh exits early the write
	// fails (EPIPE) instead of blocking docker save forever.
	gz := gzip.NewWriter(sshIn)
	_, copyErr := io.Copy(gz, saveOut)
	if copyErr == nil {
		copyErr = gz.Close()
	}
	_ = sshIn.Close()
	if copyErr != nil {
		_ = save.Process.Kill()
	}
	saveErr := save.Wait()
	sshErr := sshCmd.Wait()
	switch {
	case sshErr != nil:
		return fmt.Errorf("load image on the VPS: %w", sshErr)
	case saveErr != nil:
		return fmt.Errorf("docker save %s: %w", ref, saveErr)
	case copyErr != nil:
		return fmt.Errorf("stream image: %w", copyErr)
	}
	return nil
}

// Local runs a local command with inherited output.
func (s *Session) Local(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("empty local command")
	}
	return s.Runner.Run(Cmd{Path: args[0], Args: args[1:], Stdout: s.Out, Stderr: s.Err})
}

// OutputLocal runs a local command and captures stdout.
func (s *Session) OutputLocal(args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty local command")
	}
	return s.Runner.Output(Cmd{Path: args[0], Args: args[1:], Stderr: s.Err})
}

// ReadFile is here so callers can inject file reads in tests.
var ReadFile = os.ReadFile

// CheckIdentityFile reports a missing or unreadable private key before ssh
// prints a less obvious authentication error.
func CheckIdentityFile(path string) error {
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("identity_file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("identity_file: %s is not a regular file", path)
	}
	return nil
}
