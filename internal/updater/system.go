package updater

import (
	"bufio"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	SystemdServiceName  = "runeconsole.service"
	LaunchdServiceName  = "system/com.cryptolabinc.runeconsole"
	DefaultBinaryPath   = "/usr/local/bin/runeconsole"
	DefaultBackupDir    = "/var/backups/runeconsole"
	DefaultLaunchdPlist = "/Library/LaunchDaemons/com.cryptolabinc.runeconsole.plist"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		// Service manager output may contain its configuration command line. Do
		// not forward it because that can include secret-bearing paths.
		return nil, fmt.Errorf("%s command failed: %w", filepath.Base(name), err)
	}
	return output, nil
}

type SystemService struct {
	GOOS         string
	Runner       CommandRunner
	LaunchdPlist string
}

func (s SystemService) Stop(ctx context.Context) error {
	runner := s.Runner
	if runner == nil {
		runner = ExecCommandRunner{}
	}
	switch s.GOOS {
	case "linux":
		_, err := runner.Run(ctx, "systemctl", "stop", SystemdServiceName)
		return err
	case "darwin":
		_, err := runner.Run(ctx, "launchctl", "bootout", LaunchdServiceName)
		return err
	default:
		return fmt.Errorf("unsupported service operating system %q", s.GOOS)
	}
}

func (s SystemService) Start(ctx context.Context) error {
	runner := s.Runner
	if runner == nil {
		runner = ExecCommandRunner{}
	}
	switch s.GOOS {
	case "linux":
		_, err := runner.Run(ctx, "systemctl", "start", SystemdServiceName)
		return err
	case "darwin":
		plist := s.LaunchdPlist
		if plist == "" {
			plist = DefaultLaunchdPlist
		}
		_, err := runner.Run(ctx, "launchctl", "bootstrap", "system", plist)
		return err
	default:
		return fmt.Errorf("unsupported service operating system %q", s.GOOS)
	}
}

type HealthProbe struct {
	HTTPURL string
	TCPAddr string
	Timeout time.Duration
	Client  *http.Client
}

func (p HealthProbe) WaitHealthy(ctx context.Context) error {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if p.healthy(waitCtx) {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return errors.New("runeconsole health check timed out")
		case <-ticker.C:
		}
	}
}

func (p HealthProbe) healthy(ctx context.Context) bool {
	if p.HTTPURL != "" {
		client := p.Client
		if client == nil {
			client = &http.Client{Timeout: 2 * time.Second}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.HTTPURL, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
		return resp.StatusCode >= 200 && resp.StatusCode < 300
	}
	if p.TCPAddr != "" {
		dialer := net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", p.TCPAddr)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}
	return false
}

type FileLocker struct {
	Path string
}

func (l FileLocker) Lock() (func() error, error) {
	requestedPath, err := cleanAbsolute(l.Path)
	if err != nil {
		return nil, errors.New("update lock path must be absolute")
	}
	canonicalParent, err := canonicalizePath(filepath.Dir(requestedPath))
	if err != nil {
		return nil, errors.New("resolve update lock directory failed")
	}
	path := filepath.Join(canonicalParent, filepath.Base(requestedPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, errors.New("create update lock directory failed")
	}
	dirInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("update lock directory is unsafe")
	}
	if stat, ok := dirInfo.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() || dirInfo.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("update lock directory must be updater-owned and not group/world writable")
	}
	if err := validateSecureAncestors(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, errors.New("secure update lock directory failed")
	}
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, errors.New("open update lock failed")
	}
	f := os.NewFile(uintptr(fd), "runeconsole-update-lock")
	lockInfo, err := f.Stat()
	if err != nil || !lockInfo.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("update lock file is unsafe")
	}
	if stat, ok := lockInfo.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() || lockInfo.Mode().Perm()&0o022 != 0 {
		f.Close()
		return nil, errors.New("update lock file must be updater-owned and not group/world writable")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		f.Close()
		return nil, errors.New("secure update lock failed")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, errors.New("acquire update lock failed")
	}
	return func() error {
		unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		closeErr := f.Close()
		return errors.Join(unlockErr, closeErr)
	}, nil
}

type BinaryResolutionOptions struct {
	GOOS               string
	ExplicitPath       string
	ExecutablePath     string
	ExpectedConfigPath string
	Runner             CommandRunner
	SystemdUnitPaths   []string
	LaunchdPlistPaths  []string
}

type serviceDescriptor struct {
	binaryPath string
	configPath string
}

// ResolveServiceBinary locates the binary actually launched by the service.
// When no explicit override is supplied, invoking a copied CLI is rejected:
// otherwise the wrong inode could be replaced while the old service still
// passes its health check.
func ResolveServiceBinary(ctx context.Context, opts BinaryResolutionOptions) (string, error) {
	explicit := opts.ExplicitPath != ""
	descriptor := serviceDescriptor{}
	switch opts.GOOS {
	case "linux":
		runner := opts.Runner
		if runner == nil {
			runner = ExecCommandRunner{}
		}
		if output, err := runner.Run(ctx, "systemctl", "show", "--property=ExecStart", "--value", SystemdServiceName); err == nil {
			descriptor = descriptorFromSystemctl(string(output))
		}
		if descriptor.binaryPath == "" || descriptor.configPath == "" {
			paths := opts.SystemdUnitPaths
			for _, path := range paths {
				candidate, err := descriptorFromSystemdUnit(path)
				if err == nil && candidate.binaryPath != "" && candidate.configPath != "" {
					descriptor = candidate
					break
				}
			}
		}
	case "darwin":
		runner := opts.Runner
		if runner == nil {
			runner = ExecCommandRunner{}
		}
		loadedOutput, err := runner.Run(ctx, "launchctl", "print", LaunchdServiceName)
		if err != nil {
			return "", errors.New("could not inspect the loaded runeconsole launchd job")
		}
		loadedDescriptor := descriptorFromLaunchctlPrint(string(loadedOutput))
		if loadedDescriptor.binaryPath == "" || loadedDescriptor.configPath == "" {
			return "", errors.New("loaded runeconsole launchd job has no identifiable program or --config")
		}
		paths := opts.LaunchdPlistPaths
		if len(paths) == 0 {
			paths = []string{DefaultLaunchdPlist}
		}
		for _, path := range paths {
			candidate, err := descriptorFromLaunchdPlist(path)
			if err == nil && candidate.binaryPath != "" && candidate.configPath != "" {
				descriptor = candidate
				break
			}
		}
		if descriptor.binaryPath == "" || descriptor.configPath == "" || !descriptorsReferToSameFiles(descriptor, loadedDescriptor) {
			return "", errors.New("loaded launchd job does not match the installed runeconsole plist")
		}
	default:
		return "", fmt.Errorf("unsupported update operating system %q", opts.GOOS)
	}
	if descriptor.binaryPath == "" || descriptor.configPath == "" {
		return "", errors.New("could not identify the runeconsole service binary and --config argument")
	}
	serviceConfig, err := canonicalExistingFile(descriptor.configPath)
	if err != nil {
		return "", errors.New("service definition has an invalid --config path")
	}
	if opts.ExpectedConfigPath == "" {
		return "", errors.New("expected runeconsole config path is required")
	}
	expectedConfig, err := canonicalExistingFile(opts.ExpectedConfigPath)
	if err != nil || expectedConfig != serviceConfig {
		return "", errors.New("the service --config path does not match the config selected for update")
	}

	var target string
	if explicit {
		target = opts.ExplicitPath
	} else {
		target = descriptor.binaryPath
	}
	if target == "" || !filepath.IsAbs(target) {
		return "", errors.New("could not identify an absolute runeconsole service binary; use --binary-path")
	}
	target, err = canonicalExistingBinary(target)
	if err != nil {
		return "", err
	}
	if explicit {
		serviceTarget, discoverErr := canonicalExistingBinary(descriptor.binaryPath)
		if discoverErr != nil {
			return "", errors.New("identified service binary is unsafe")
		}
		if serviceTarget != target {
			return "", errors.New("--binary-path does not match the service definition")
		}
	}
	if !explicit {
		running, err := canonicalExistingBinary(opts.ExecutablePath)
		if err != nil {
			return "", errors.New("could not identify the running runeconsole executable; use --binary-path")
		}
		if running != target {
			return "", errors.New("the invoked executable is not the service binary; run the installed binary or pass --binary-path explicitly")
		}
	}
	return target, nil
}

func descriptorFromLaunchctlPrint(output string) serviceDescriptor {
	var descriptor serviceDescriptor
	var arguments []string
	inArguments := false
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "program = ") {
			descriptor.binaryPath = strings.Trim(strings.TrimPrefix(line, "program = "), "\"")
			continue
		}
		if line == "arguments = {" {
			inArguments = true
			continue
		}
		if inArguments && line == "}" {
			inArguments = false
			continue
		}
		if inArguments && line != "" {
			arguments = append(arguments, strings.Trim(line, "\""))
		}
	}
	if descriptor.binaryPath == "" && len(arguments) > 0 {
		descriptor.binaryPath = arguments[0]
	}
	descriptor.configPath = configArgument(arguments)
	return descriptor
}

func descriptorsReferToSameFiles(a, b serviceDescriptor) bool {
	aBinary, aBinaryErr := canonicalExistingBinary(a.binaryPath)
	bBinary, bBinaryErr := canonicalExistingBinary(b.binaryPath)
	aConfig, aConfigErr := canonicalExistingFile(a.configPath)
	bConfig, bConfigErr := canonicalExistingFile(b.configPath)
	return aBinaryErr == nil && bBinaryErr == nil && aConfigErr == nil && bConfigErr == nil && aBinary == bBinary && aConfig == bConfig
}

func canonicalExistingFile(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("path is not a regular file")
	}
	return filepath.Clean(resolved), nil
}

func canonicalExistingBinary(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("runeconsole binary path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("runeconsole binary path does not exist")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("runeconsole binary path is not an executable regular file")
	}
	return filepath.Clean(resolved), nil
}

func parseSystemctlExecStart(output string) string {
	return descriptorFromSystemctl(output).binaryPath
}

func descriptorFromSystemctl(output string) serviceDescriptor {
	descriptor := serviceDescriptor{}
	marker := "path="
	if index := strings.Index(output, marker); index >= 0 {
		rest := output[index+len(marker):]
		if end := strings.IndexAny(rest, " ;\n\t"); end >= 0 {
			rest = rest[:end]
		}
		if filepath.IsAbs(rest) {
			descriptor.binaryPath = rest
		}
	}
	if index := strings.Index(output, "argv[]="); index >= 0 {
		rest := output[index+len("argv[]="):]
		if end := strings.Index(rest, " ;"); end >= 0 {
			rest = rest[:end]
		}
		words := splitCommandWords(rest)
		if descriptor.binaryPath == "" && len(words) > 0 {
			descriptor.binaryPath = strings.TrimLeft(words[0], "-@:+!")
		}
		descriptor.configPath = configArgument(words)
	}
	return descriptor
}

func descriptorFromSystemdUnit(path string) (serviceDescriptor, error) {
	f, err := os.Open(path)
	if err != nil {
		return serviceDescriptor{}, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "ExecStart=") {
			words := splitCommandWords(strings.TrimPrefix(line, "ExecStart="))
			if len(words) == 0 {
				return serviceDescriptor{}, errors.New("empty ExecStart")
			}
			return serviceDescriptor{
				binaryPath: strings.TrimLeft(words[0], "-@:+!"),
				configPath: configArgument(words),
			}, nil
		}
	}
	return serviceDescriptor{}, scanner.Err()
}

func binaryFromSystemdUnit(path string) (string, error) {
	descriptor, err := descriptorFromSystemdUnit(path)
	return descriptor.binaryPath, err
}

func firstCommandWord(command string) string {
	words := splitCommandWords(command)
	if len(words) == 0 {
		return ""
	}
	return strings.TrimLeft(words[0], "-@:+!")
}

func splitCommandWords(command string) []string {
	command = strings.TrimSpace(command)
	var b strings.Builder
	var words []string
	var quote rune
	escaped := false
	flush := func() {
		if b.Len() > 0 {
			words = append(words, b.String())
			b.Reset()
		}
	}
	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return words
}

func descriptorFromLaunchdPlist(path string) (serviceDescriptor, error) {
	f, err := os.Open(path)
	if err != nil {
		return serviceDescriptor{}, err
	}
	defer f.Close()
	decoder := xml.NewDecoder(f)
	wantArray := false
	inArray := false
	var arguments []string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if len(arguments) == 0 {
				return serviceDescriptor{}, nil
			}
			return serviceDescriptor{binaryPath: arguments[0], configPath: configArgument(arguments)}, nil
		}
		if err != nil {
			return serviceDescriptor{}, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "key":
				var key string
				if err := decoder.DecodeElement(&key, &value); err != nil {
					return serviceDescriptor{}, err
				}
				wantArray = strings.TrimSpace(key) == "ProgramArguments"
			case "array":
				if wantArray {
					inArray = true
					wantArray = false
				}
			case "string":
				if inArray {
					var binary string
					if err := decoder.DecodeElement(&binary, &value); err != nil {
						return serviceDescriptor{}, err
					}
					arguments = append(arguments, strings.TrimSpace(binary))
				}
			}
		case xml.EndElement:
			if value.Name.Local == "array" && inArray {
				if len(arguments) == 0 {
					return serviceDescriptor{}, nil
				}
				return serviceDescriptor{binaryPath: arguments[0], configPath: configArgument(arguments)}, nil
			}
		}
	}
}

func binaryFromLaunchdPlist(path string) (string, error) {
	descriptor, err := descriptorFromLaunchdPlist(path)
	return descriptor.binaryPath, err
}

func configArgument(arguments []string) string {
	for i, argument := range arguments {
		if argument == "--config" && i+1 < len(arguments) {
			return arguments[i+1]
		}
		if strings.HasPrefix(argument, "--config=") {
			return strings.TrimPrefix(argument, "--config=")
		}
	}
	return ""
}
