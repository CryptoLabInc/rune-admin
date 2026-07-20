package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuneconsoleSystemdSandboxKeepsUpdateHandoffOnOneMount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		wantMount     string
		separateMount string
	}{
		{
			name:          "checked-in unit",
			path:          filepath.Join("..", "..", "deployment", "systemd", "runeconsole.service"),
			wantMount:     "ReadWritePaths=/opt/runeconsole /var/lib/runeconsole-updater",
			separateMount: "ReadWritePaths=/opt/runeconsole /var/lib/runeconsole-updater/inbox /var/lib/runeconsole-updater/staging",
		},
		{
			name:          "installer-generated unit",
			path:          filepath.Join("..", "..", "install.sh"),
			wantMount:     `"ReadWritePaths=${INSTALL_PREFIX} ${UPDATE_STATE_DIR}"`,
			separateMount: `"ReadWritePaths=${INSTALL_PREFIX} ${UPDATE_INBOX_DIR} ${UPDATE_STAGING_DIR}"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			unit := string(body)
			if strings.Contains(unit, test.separateMount) {
				t.Fatalf("%s gives inbox and staging separate systemd bind mounts; link(2) cannot publish the staged request across them", test.path)
			}
			if !strings.Contains(unit, test.wantMount) {
				t.Fatalf("%s does not expose the shared updater root as one writable systemd mount", test.path)
			}
		})
	}
}
