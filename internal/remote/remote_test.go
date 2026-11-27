package remote

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type recordRunner struct {
	cmds   []Cmd
	stdin  [][]byte
	output []byte
}

func (r *recordRunner) Run(c Cmd) error {
	r.cmds = append(r.cmds, c)
	if c.Stdin != nil {
		b, _ := io.ReadAll(c.Stdin)
		r.stdin = append(r.stdin, b)
	}
	return nil
}

func (r *recordRunner) Output(c Cmd) ([]byte, error) {
	r.cmds = append(r.cmds, c)
	return r.output, nil
}

func TestSSHArgv(t *testing.T) {
	got := SSHArgv("ops@203.0.113.10", "", "sudo -n true", false)
	want := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "ops@203.0.113.10", "sudo -n true"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("SSHArgv = %#v, want %#v", got, want)
	}
	tty := SSHArgv("ops@host", "", "sudo -n true", true)
	if !contains(tty, "-t") {
		t.Fatalf("TTY argv lacks -t: %#v", tty)
	}
}

func TestLocalAgentCommandAsRootIgnoresUser(t *testing.T) {
	withLocalIdentity(t, 0, "root")
	s := &Session{LocalMode: true, User: "ops"}
	program, argv := s.AgentCommand(false, "status")
	if program != agentPath || strings.Join(argv, "\x00") != "status" {
		t.Fatalf("local root command = %q %#v", program, argv)
	}
}

func TestLocalAgentCommandUsesSudo(t *testing.T) {
	withLocalIdentity(t, 1000, "dev")
	for _, user := range []string{"", "dev"} {
		s := &Session{LocalMode: true, User: user}
		program, argv := s.AgentCommand(false, "status")
		want := []string{"-n", agentPath, "status"}
		if program != "sudo" || strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("user %q command = %q %#v", user, program, argv)
		}
	}
}

func TestLocalAgentCommandBecomesUserThenRoot(t *testing.T) {
	withLocalIdentity(t, 1000, "dev")
	s := &Session{LocalMode: true, User: "ops"}
	program, argv := s.AgentCommand(false, "status")
	want := []string{"-n", "-u", "ops", "sudo", "-n", agentPath, "status"}
	if program != "sudo" || strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("command = %q %#v", program, argv)
	}

	program, argv = s.AgentCommand(true, "logs", "app")
	want = []string{"-u", "ops", "sudo", agentPath, "logs", "app"}
	if program != "sudo" || strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("TTY command = %q %#v", program, argv)
	}
}

func TestLocalShellAndUploadUseSudo(t *testing.T) {
	withLocalIdentity(t, 1000, "dev")
	s := &Session{LocalMode: true, Out: io.Discard, Err: io.Discard, Runner: &recordRunner{}}
	program, argv := s.ShellCommand("true", false)
	if program != "sudo" || strings.Join(argv, "\x00") != "-n\x00sh\x00-c\x00true" {
		t.Fatalf("shell command = %q %#v", program, argv)
	}

	r := &recordRunner{}
	s.Runner = r
	if err := s.UploadFile("/usr/local/sbin/ship-agent", 0o755, []byte("agent")); err != nil {
		t.Fatal(err)
	}
	if len(r.cmds) != 1 {
		t.Fatalf("upload commands = %d", len(r.cmds))
	}
	up := r.cmds[0]
	joined := strings.Join(up.Args, " ")
	if up.Path != "sudo" || !strings.Contains(joined, "mkdir -p '/usr/local/sbin'") ||
		!strings.Contains(joined, "'/usr/local/sbin/ship-agent' '0755'") ||
		!strings.Contains(joined, `mv -f "$t" "$1"`) {
		t.Fatalf("upload command = %q %#v", up.Path, up.Args)
	}
	if len(r.stdin) != 1 || string(r.stdin[0]) != "agent" {
		t.Fatalf("upload stdin = %q", r.stdin)
	}
}

func TestLocalShipImageSkipsTransfer(t *testing.T) {
	withLocalIdentity(t, 1000, "dev")
	r := &recordRunner{}
	s := &Session{LocalMode: true, Out: io.Discard, Err: io.Discard, Runner: r}
	if err := s.ShipImage("ship/app:v1", "image-load", "app", "v1"); err != nil {
		t.Fatal(err)
	}
	if len(r.cmds) != 1 {
		t.Fatalf("local image commands = %d", len(r.cmds))
	}
	cmd := r.cmds[0]
	if cmd.Path != "docker" || strings.Join(cmd.Args, " ") != "image inspect ship/app:v1" {
		t.Fatalf("local image command = %q %#v", cmd.Path, cmd.Args)
	}
}

func withLocalIdentity(t *testing.T, euid int, user string) {
	t.Helper()
	oldEUID, oldUser := geteuid, currentUser
	geteuid = func() int { return euid }
	currentUser = func() string { return user }
	t.Cleanup(func() {
		geteuid, currentUser = oldEUID, oldUser
	})
}

func TestAgentRemote(t *testing.T) {
	got := AgentRemote("deploy", "lewbox", "v1.2.3")
	want := `sudo -n /usr/local/sbin/ship-agent 'deploy' 'lewbox' 'v1.2.3'`
	if got != want {
		t.Fatalf("AgentRemote = %q, want %q", got, want)
	}
	if q := ShellQuote("a'b"); q != `'a'\''b'` {
		t.Fatalf("ShellQuote = %q", q)
	}
}

func probeOutput(version int, sum string) []byte {
	return []byte("version=" + strconv.Itoa(version) + "\nsha256=" + sum + "\n")
}

func TestEnsureAgentUploadsWhenHashDiffers(t *testing.T) {
	r := &recordRunner{output: probeOutput(AgentProtocol, "deadbeef")}
	s := &Session{Host: "ops@host", Out: io.Discard, Err: io.Discard, Runner: r}
	agent := []byte("#!/usr/bin/env bash\necho hi\n")
	if err := s.EnsureAgent(agent); err != nil {
		t.Fatal(err)
	}
	if len(r.cmds) != 2 {
		t.Fatalf("want 2 commands (probe, upload), got %d", len(r.cmds))
	}
	probe := strings.Join(r.cmds[0].Args, " ")
	if !strings.Contains(probe, "AGENT_VERSION") || !strings.Contains(probe, "sha256sum") {
		t.Fatalf("probe command = %q", probe)
	}
	up := r.cmds[1]
	joined := strings.Join(up.Args, " ")
	if !strings.Contains(joined, "/usr/local/sbin/ship-agent") || !strings.HasSuffix(joined, "'/usr/local/sbin/ship-agent' '0755'") ||
		!strings.Contains(joined, `mv -f "$t" "$1"`) {
		t.Fatalf("upload command = %q", joined)
	}
	if strings.Contains(joined, "install -d") || !strings.Contains(joined, "mkdir -p") {
		t.Fatalf("upload must not chmod existing parents: %q", joined)
	}
	if len(r.stdin) != 1 || !bytes.Equal(r.stdin[0], agent) {
		t.Fatal("agent bytes were not streamed on stdin")
	}
}

func TestEnsureAgentSkipsMatchingHash(t *testing.T) {
	script := []byte("script")
	r := &recordRunner{output: probeOutput(AgentProtocol, shaForTest(script))}
	s := &Session{Host: "ops@host", Out: io.Discard, Err: io.Discard, Runner: r}
	if err := s.EnsureAgent(script); err != nil {
		t.Fatal(err)
	}
	if len(r.cmds) != 1 {
		t.Fatalf("matching hash should not upload, got %d commands", len(r.cmds))
	}
}

func TestEnsureAgentRefusesNewerAgent(t *testing.T) {
	r := &recordRunner{output: probeOutput(AgentProtocol+1, "deadbeef")}
	s := &Session{Host: "ops@host", Out: io.Discard, Err: io.Discard, Runner: r}
	err := s.EnsureAgent([]byte("script"))
	if err == nil || !strings.Contains(err.Error(), "newer") || !strings.Contains(err.Error(), "ship update") {
		t.Fatalf("err = %v", err)
	}
	if len(r.cmds) != 1 {
		t.Fatalf("newer agent must not be overwritten, got %d commands", len(r.cmds))
	}
}

func TestEnsureAgentReinstallsSameVersionDifferentHash(t *testing.T) {
	r := &recordRunner{output: probeOutput(AgentProtocol, "deadbeef")}
	s := &Session{Host: "ops@host", Out: io.Discard, Err: io.Discard, Runner: r}
	if err := s.EnsureAgent([]byte("script")); err != nil {
		t.Fatal(err)
	}
	if len(r.cmds) != 2 {
		t.Fatalf("want reinstall, got %d commands", len(r.cmds))
	}
}

func TestEnsureAgentUpgradesOlderAgent(t *testing.T) {
	r := &recordRunner{output: probeOutput(AgentProtocol-1, shaForTest([]byte("script")))}
	s := &Session{Host: "ops@host", Out: io.Discard, Err: io.Discard, Runner: r}
	if err := s.EnsureAgent([]byte("script")); err != nil {
		t.Fatal(err)
	}
	if len(r.cmds) != 2 {
		t.Fatalf("older agent should be upgraded, got %d commands", len(r.cmds))
	}
}

func TestAgentProtocolMatchesScript(t *testing.T) {
	b, err := os.ReadFile("../../ship-agent")
	if err != nil {
		t.Fatal(err)
	}
	want := "\nAGENT_VERSION=" + strconv.Itoa(AgentProtocol) + "\n"
	if !strings.Contains(string(b), want) {
		t.Fatalf("ship-agent does not declare AGENT_VERSION=%d", AgentProtocol)
	}
}

func TestUploadFileDryRunPrintsCommand(t *testing.T) {
	var out bytes.Buffer
	s := NewSession("ops@host", true, &out, io.Discard)
	if err := s.UploadFile("/opt/apps/x/compose.yaml", 0o644, []byte("name: x\n")); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"$ 'ssh'", "/opt/apps/x/compose.yaml", "stdin: 8 bytes"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, got)
		}
	}
}

func TestDryRunAgentArgv(t *testing.T) {
	var out bytes.Buffer
	s := NewSession("ops@host", true, &out, io.Discard)
	if err := s.RunAgent("deploy", "lewbox", "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"ship-agent", "lewbox", "v1.2.3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dry-run argv missing %q: %q", want, got)
		}
	}
}

func TestShipImageDryRun(t *testing.T) {
	var out bytes.Buffer
	s := NewSession("ops@host", true, &out, io.Discard)
	if err := s.ShipImage("ship/app:v1.2.3", "image-load", "app", "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"docker", "save", "ship/app:v1.2.3", "gzip", "ssh", "image-load", "app", "v1.2.3"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, got)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func shaForTest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestSSHArgvIdentityFile(t *testing.T) {
	got := SSHArgv("ops@host", "/home/me/.ssh/prod", "true", false)
	want := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-i", "/home/me/.ssh/prod", "-o", "IdentitiesOnly=yes", "ops@host", "true"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SSHArgv = %#v, want %#v", got, want)
	}
}

func TestEnsureAgentRefusesEmptyAgent(t *testing.T) {
	r := &recordRunner{output: probeOutput(AgentProtocol, "deadbeef")}
	s := &Session{Host: "ops@host", Out: io.Discard, Err: io.Discard, Runner: r}
	if err := s.EnsureAgent(nil); err == nil {
		t.Fatal("expected an error for an empty agent")
	}
	if len(r.cmds) != 0 {
		t.Fatalf("must not contact the server, ran %d commands", len(r.cmds))
	}
}
