// integrations.go — MODERN (v1alpha) integration/connector/job discovery.
//
// SOAR ships integrations (e.g. the VirusTotal or Slack packs); each bundles
// connector definitions (ingestion) and job definitions (scheduled work). These
// list endpoints are read-only catalogs used to drive configuration tooling.

package soar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"danny.vn/secops/soar/internal/transport"
)

// Integration is an installed SOAR integration (a.k.a. an IDE/marketplace pack).
//
// GOTCHA: an integration that has been *cloned* in the tenant keeps the original
// pack's Identifier but gets a suffixed Name of the form "Name__<uuid>" (e.g.
// "VirusTotal__1a2b3c..."). Address clones by Name; the base pack by Identifier.
// Treat Name as the addressable key for GetIntegration / nested lists.
type Integration struct {
	Name            string          `json:"name"`
	Identifier      string          `json:"identifier"`
	DisplayName     string          `json:"displayName"`
	LatestVersion   string          `json:"latestVersion"`
	UpdateAvailable bool            `json:"updateAvailable"`
	Raw             json.RawMessage `json:"-"` // full server object, untrimmed
}

// UnmarshalJSON decodes the typed fields while preserving the complete server
// object in Raw (the v1alpha integration payload carries far more than we type).
func (i *Integration) UnmarshalJSON(data []byte) error {
	type alias Integration // avoid recursion
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*i = Integration(a)
	i.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ConnectorDef is a connector definition within an integration (an ingestion
// source template, not a configured instance). ID is the numeric definition id
// used in the connector's resource path (e.g. ".../connectors/48").
type ConnectorDef struct {
	Name        string          `json:"name"`
	ID          string          `json:"id"`
	Identifier  string          `json:"identifier"`
	DisplayName string          `json:"displayName"`
	Raw         json.RawMessage `json:"-"`
}

// PathID returns the segment that addresses this connector in a resource path:
// the numeric ID when present, else the last segment of Name, else Identifier.
func (c *ConnectorDef) PathID() string { return pathID(c.ID, c.Name, c.Identifier) }

// UnmarshalJSON decodes the typed fields and keeps the full object in Raw.
func (c *ConnectorDef) UnmarshalJSON(data []byte) error {
	type alias ConnectorDef
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = ConnectorDef(a)
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// JobDef is a job definition within an integration (a scheduled-task template).
// ID is the numeric definition id used in the job's resource path.
type JobDef struct {
	Name        string          `json:"name"`
	ID          string          `json:"id"`
	Identifier  string          `json:"identifier"`
	DisplayName string          `json:"displayName"`
	Raw         json.RawMessage `json:"-"`
}

// PathID returns the segment that addresses this job in a resource path.
func (j *JobDef) PathID() string { return pathID(j.ID, j.Name, j.Identifier) }

// pathID picks the resource-path segment from the available identifiers: a
// numeric id when present, else the last "/"-segment of the resource name, else
// the identifier. SOAR connector/job definitions are addressed by a numeric id.
func pathID(id, name, identifier string) string {
	if id != "" {
		return id
	}
	if name != "" {
		if i := strings.LastIndex(name, "/"); i >= 0 {
			return name[i+1:]
		}
		return name
	}
	return identifier
}

// UnmarshalJSON decodes the typed fields and keeps the full object in Raw.
func (j *JobDef) UnmarshalJSON(data []byte) error {
	type alias JobDef
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*j = JobDef(a)
	j.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// listMaxPages caps {items,nextPageToken} pagination to avoid runaway loops.
const listMaxPages = 50

// integrationsList is the v1alpha LIST envelope for integrations.
type integrationsList struct {
	Items         []Integration `json:"integrations"`
	NextPageToken string        `json:"nextPageToken"`
}

// connectorsList is the v1alpha LIST envelope for connector definitions.
type connectorsList struct {
	Items         []ConnectorDef `json:"connectors"`
	NextPageToken string         `json:"nextPageToken"`
}

// jobsList is the v1alpha LIST envelope for job definitions.
type jobsList struct {
	Items         []JobDef `json:"jobs"`
	NextPageToken string   `json:"nextPageToken"`
}

// ListIntegrations returns every installed integration in the tenant.
func (c *Client) ListIntegrations(ctx context.Context) ([]Integration, error) {
	var all []Integration
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page integrationsList
		if err := c.t.V1Alpha(ctx, "GET", "integrations", nil, &page, pageTokenOpt(token)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// GetIntegration fetches a single integration by its addressable key (Name for a
// clone, Identifier for a base pack — see the Integration gotcha).
func (c *Client) GetIntegration(ctx context.Context, identifier string) (*Integration, error) {
	var out Integration
	if err := c.t.V1Alpha(ctx, "GET", "integrations/"+identifier, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListConnectors returns the connector definitions of one integration. integration
// is the addressable key (Name/Identifier — see the Integration gotcha).
func (c *Client) ListConnectors(ctx context.Context, integration string) ([]ConnectorDef, error) {
	var all []ConnectorDef
	res := fmt.Sprintf("integrations/%s/connectors", integration)
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page connectorsList
		if err := c.t.V1Alpha(ctx, "GET", res, nil, &page, pageTokenOpt(token)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// ListJobs returns the job definitions of one integration. integration is the
// addressable key (Name/Identifier — see the Integration gotcha).
func (c *Client) ListJobs(ctx context.Context, integration string) ([]JobDef, error) {
	var all []JobDef
	res := fmt.Sprintf("integrations/%s/jobs", integration)
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page jobsList
		if err := c.t.V1Alpha(ctx, "GET", res, nil, &page, pageTokenOpt(token)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// pageTokenOpt builds the transport option carrying a pagination token. On the
// first page (empty token) it yields an empty Query, a harmless no-op the
// transport folds into its query map. DEVIATION: the transport exposes
// pagination via the generic Query option rather than a dedicated page-token
// knob, and transport.Option wraps an unexported type so a true no-op can't be
// constructed here — an empty url.Values is the clean equivalent. Shared across
// the package's v1alpha listers.
func pageTokenOpt(token string) transport.Option {
	if token == "" {
		return transport.Query(url.Values{})
	}
	return transport.Query(url.Values{"pageToken": {token}})
}
