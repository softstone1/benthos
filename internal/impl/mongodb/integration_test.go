package mongodb_test

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/Jeffail/benthos/v3/internal/impl/mongodb/client"
	"github.com/Jeffail/benthos/v3/internal/integration"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func generateCollectionName(testID string) string {
	return regexp.MustCompile("[^a-zA-Z]+").ReplaceAllString(testID, "")
}

func TestIntegrationMongoDB(t *testing.T) {
	integration.CheckSkip(t)
	t.Parallel()

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)

	pool.MaxWait = time.Second * 30

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "mongo",
		Tag:        "latest",
		Env: []string{
			"MONGO_INITDB_ROOT_USERNAME=mongoadmin",
			"MONGO_INITDB_ROOT_PASSWORD=secret",
		},
		ExposedPorts: []string{"27017"},
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		assert.NoError(t, pool.Purge(resource))
	})

	var mongoClient *mongo.Client

	resource.Expire(900)
	require.NoError(t, pool.Retry(func() error {
		url := "mongodb://localhost:" + resource.GetPort("27017/tcp")
		conf := client.NewConfig()
		conf.URL = url
		conf.Database = "TestDB"
		conf.Collection = "TestCollection"
		conf.Username = "mongoadmin"
		conf.Password = "secret"

		mongoClient, err = conf.Client()
		if err != nil {
			return err
		}
		return mongoClient.Connect(context.Background())
	}))

	t.Run("with JSON", func(t *testing.T) {
		template := `
output:
  mongodb:
    url: mongodb://localhost:$PORT
    database: TestDB
    collection: $VAR1
    username: mongoadmin
    password: secret
    operation: insert-one
    document_map: |
      root.id = this.id
      root.content = this.content
    write_concern:
      w: 1
      w_timeout: 1s
`
		queryGetFn := func(ctx context.Context, testID, messageID string) (string, []string, error) {
			db := mongoClient.Database("TestDB")
			collection := db.Collection(generateCollectionName(testID))
			idInt, err := strconv.Atoi(messageID)
			if err != nil {
				return "", nil, err
			}

			filter := bson.M{"id": idInt}
			document, err := collection.FindOne(context.Background(), filter).DecodeBytes()
			if err != nil {
				return "", nil, err
			}

			value, err := document.LookupErr("content")
			if err != nil {
				return "", nil, err
			}

			return fmt.Sprintf(`{"content":%v,"id":%v}`, value.String(), messageID), nil, err
		}

		suite := integration.StreamTests(
			integration.StreamTestOutputOnlySendSequential(10, queryGetFn),
			integration.StreamTestOutputOnlySendBatch(10, queryGetFn),
		)
		suite.Run(
			t, template,
			integration.StreamTestOptPort(resource.GetPort("27017/tcp")),
			integration.StreamTestOptPreTest(func(t testing.TB, ctx context.Context, testID string, vars *integration.StreamTestConfigVars) {
				cName := generateCollectionName(testID)
				vars.Var1 = cName
				require.NoError(t, createCollection(resource, cName, "mongoadmin", "secret"))
			}),
		)
	})
}