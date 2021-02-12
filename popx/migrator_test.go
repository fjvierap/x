package popx

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/gobuffalo/pop/v5"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ory/x/logrusx"
	"github.com/ory/x/pkgerx"
	"github.com/ory/x/sqlcon/dockertest"
)

func TestMigratorUpgrading(t *testing.T) {
	litedb, err := ioutil.TempFile(os.TempDir(), "sqlite-*")
	require.NoError(t, err)
	require.NoError(t, litedb.Close())

	sqlite, err := pop.NewConnection(&pop.ConnectionDetails{
		URL: "sqlite://file::memory:?_fk=true",
	})
	require.NoError(t, err)
	require.NoError(t, sqlite.Open())

	connections := map[string]*pop.Connection{
		"sqlite": sqlite,
	}

	if !testing.Short() {
		dockertest.Parallel([]func(){
			func() {
				connections["postgres"] = dockertest.ConnectToTestPostgreSQLPop(t)
			},
			func() {
				connections["mysql"] = dockertest.ConnectToTestMySQLPop(t)
			},
			func() {
				connections["cockroach"] = dockertest.ConnectToTestCockroachDBPop(t)
			},
		})
	}

	l := logrusx.New("", "", logrusx.ForceLevel(logrus.DebugLevel))

	for name, c := range connections {
		t.Run(fmt.Sprintf("database=%s", name), func(t *testing.T) {
			legacy, err := pkgerx.NewMigrationBox("/popx/stub/migrations/legacy", c, l)
			require.NoError(t, err)
			require.NoError(t, legacy.Up())

			var legacyStatusBuffer bytes.Buffer
			require.NoError(t, legacy.Status(&legacyStatusBuffer))

			legacyStatus := filterMySQL(t, name, legacyStatusBuffer.String())

			require.NotContains(t, legacyStatus, "Pending")

			expected := legacy.DumpMigrationSchema()

			transactional, err := NewMigrationBoxPkger("/popx/stub/migrations/transactional", c, l)
			require.NoError(t, err)

			var transactionalStatusBuffer bytes.Buffer
			require.NoError(t, transactional.Status(&transactionalStatusBuffer))

			transactionalStatus := filterMySQL(t, name, transactionalStatusBuffer.String())
			require.NotContains(t, transactionalStatus, "Pending")

			require.NoError(t, transactional.Up())

			actual := transactional.DumpMigrationSchema()
			assert.EqualValues(t, expected, actual)

			// Re-set and re-try

			require.NoError(t, legacy.Down(-1))
			require.NoError(t, transactional.Up())
			actual = transactional.DumpMigrationSchema()
			assert.EqualValues(t, expected, actual)
		})
	}
}

func filterMySQL(t *testing.T, name string, status string) string {
	if name == "mysql" {
		return status
	}
	// These only run for mysql and are thus expected to be pending:
	//
	// 20191100000005   identities                                Pending
	// 20191100000009   verification                              Pending
	// 20200519101058   create_recovery_addresses                 Pending
	// 20200601101001   verification                              Pending

	pending := []string{"20191100000005", "20191100000009", "20200519101058", "20200601101001"}
	var lines []string
	for _, l := range strings.Split(status, "\n") {
		var skip bool
		for _, p := range pending {
			if strings.Contains(l, p) {
				t.Logf("Removing expected pending line: %s", l)
				skip = true
				break
			}
		}
		if !skip {
			lines = append(lines, l)
		}
	}

	return strings.Join(lines, "\n")
}

func TestMigratorUpgradingFromStart(t *testing.T) {
	litedb, err := ioutil.TempFile(os.TempDir(), "sqlite-*")
	require.NoError(t, err)
	require.NoError(t, litedb.Close())

	c, err := pop.NewConnection(&pop.ConnectionDetails{
		URL: "sqlite://file::memory:?_fk=true",
	})
	require.NoError(t, err)
	require.NoError(t, c.Open())

	l := logrusx.New("", "", logrusx.ForceLevel(logrus.DebugLevel))
	transactional, err := NewMigrationBoxPkger("/popx/stub/migrations/transactional", c, l)
	require.NoError(t, err)
	require.NoError(t, transactional.Up())
}