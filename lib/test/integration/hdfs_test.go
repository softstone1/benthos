package integration

import (
	"testing"
	"time"

	"github.com/Jeffail/benthos/v3/internal/integration"
	"github.com/colinmarc/hdfs"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerIntegrationTest("hdfs", func(t *testing.T) {
	t.Skip() // Skip until we fix the static port bindings
	t.Parallel()

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)

	pool.MaxWait = time.Second * 30

	options := &dockertest.RunOptions{
		Repository:   "cybermaggedon/hadoop",
		Tag:          "2.8.2",
		Hostname:     "localhost",
		ExposedPorts: []string{"9000", "50075", "50070", "50010"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"9000/tcp":  {{HostIP: "", HostPort: "9000"}},
			"50070/tcp": {{HostIP: "", HostPort: "50070"}},
			"50075/tcp": {{HostIP: "", HostPort: "50075"}},
			"50010/tcp": {{HostIP: "", HostPort: "50010"}},
		},
	}
	resource, err := pool.RunWithOptions(options)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, pool.Purge(resource))
	})

	resource.Expire(900)
	require.NoError(t, pool.Retry(func() error {
		testFile := "/cluster_ready" + time.Now().Format("20060102150405")
		client, err := hdfs.NewClient(hdfs.ClientOptions{
			Addresses: []string{"localhost:9000"},
			User:      "root",
		})
		if err != nil {
			return err
		}
		fw, err := client.Create(testFile)
		if err != nil {
			return err
		}
		_, err = fw.Write([]byte("testing hdfs reader"))
		if err != nil {
			return err
		}
		err = fw.Close()
		if err != nil {
			return err
		}
		client.Remove(testFile)
		return nil
	}))

	template := `
output:
  hdfs:
    hosts: [ localhost:9000 ]
    user: root
    directory: /$ID
    path: ${!count("$ID")}-${!timestamp_unix_nano()}.txt
    max_in_flight: $MAX_IN_FLIGHT
    batching:
      count: $OUTPUT_BATCH_COUNT

input:
  hdfs:
    hosts: [ localhost:9000 ]
    user: root
    directory: /$ID
`
	integration.StreamTests(
		integration.StreamTestOpenCloseIsolated(),
		integration.StreamTestStreamIsolated(10),
		integration.StreamTestSendBatchCountIsolated(10),
	).Run(t, template)
})
