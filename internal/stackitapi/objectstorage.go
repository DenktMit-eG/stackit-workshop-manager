// Package stackitapi holds direct STACKIT API calls (via the official Go SDK) authenticated with a
// service account key, for things the terraform provider does not handle well.
package stackitapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stackitcloud/stackit-sdk-go/core/config"
	objectstorage "github.com/stackitcloud/stackit-sdk-go/services/objectstorage/v2api"
)

// EnableObjectStorage enables the Object Storage service for the project (idempotent) and waits
// until the project reports as enabled. The terraform provider tries to do this implicitly when the
// first bucket is created, which fails when several buckets race (500 / 409 "Two concurrent calls").
func EnableObjectStorage(ctx context.Context, keyJSONPath, pemPath, region, projectID string, logf func(string, ...any)) error {
	client, err := objectstorage.NewAPIClient(
		config.WithServiceAccountKeyPath(keyJSONPath),
		config.WithPrivateKeyPath(pemPath),
	)
	if err != nil {
		return fmt.Errorf("object storage client: %w", err)
	}
	if st, err := client.DefaultAPI.GetServiceStatus(ctx, projectID, region).Execute(); err == nil && st != nil {
		logf("object storage already enabled for project %s (scope %s)", projectID, st.Scope)
		return nil
	}
	logf("enabling object storage for project %s", projectID)
	if _, err := client.DefaultAPI.EnableService(ctx, projectID, region).Execute(); err != nil {
		// a concurrent/previous enablement in progress shows up as 409; keep polling in that case
		if !strings.Contains(err.Error(), "409") {
			return fmt.Errorf("enable object storage: %w", err)
		}
		logf("object storage enablement already in progress")
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		st, err := client.DefaultAPI.GetServiceStatus(ctx, projectID, region).Execute()
		if err == nil && st != nil {
			logf("object storage enabled for project %s", projectID)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("object storage not enabled after 5 minutes: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}
