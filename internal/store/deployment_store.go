package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DeploymentRecord represents a single deployment.
type DeploymentRecord struct {
	ID           string    `json:"id"`
	ProjectName  string    `json:"projectName"`
	ServerID     string    `json:"serverId"`
	ServerName   string    `json:"serverName"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	Domain       string    `json:"domain,omitempty"`
	Status       string    `json:"status"`       // "success", "failed"
	HealthStatus string    `json:"healthStatus"` // "healthy", "unhealthy", "unknown"
	DeployedAt   time.Time `json:"deployedAt"`
	LastChecked  time.Time `json:"lastChecked,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// ---- JSONL line types ----

type deplLine struct {
	Type string `json:"type"` // "deployment"
	// deployment fields
	ID           string    `json:"id,omitempty"`
	ProjectName  string    `json:"projectName,omitempty"`
	ServerID     string    `json:"serverId,omitempty"`
	ServerName   string    `json:"serverName,omitempty"`
	Host         string    `json:"host,omitempty"`
	Port         int       `json:"port,omitempty"`
	Domain       string    `json:"domain,omitempty"`
	Status       string    `json:"status,omitempty"`
	HealthStatus string    `json:"healthStatus,omitempty"`
	DeployedAt   time.Time `json:"deployedAt,omitempty"`
	LastChecked  time.Time `json:"lastChecked,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// DeploymentStore persists deployment records to disk in JSONL format.
type DeploymentStore struct {
	dir string
}

// NewDeploymentStore creates a store rooted at workDir/.agentd/deployments/.
func NewDeploymentStore(workDir string) *DeploymentStore {
	dir := filepath.Join(workDir, ".agentd", "deployments")
	os.MkdirAll(dir, 0700)
	return &DeploymentStore{dir: dir}
}

// path returns the JSONL file path for a given deployment ID.
func (s *DeploymentStore) path(id string) string {
	return filepath.Join(s.dir, id+".jsonl")
}

// List returns all deployment records, most recent first.
func (s *DeploymentStore) List() ([]DeploymentRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []DeploymentRecord{}, nil
		}
		return nil, err
	}

	var records []DeploymentRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		rec, err := s.load(e.Name())
		if err != nil {
			continue
		}
		records = append(records, rec)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].DeployedAt.After(records[j].DeployedAt)
	})

	if records == nil {
		records = []DeploymentRecord{}
	}
	return records, nil
}

// Create persists a new deployment record.
func (s *DeploymentStore) Create(rec DeploymentRecord) (*DeploymentRecord, error) {
	if rec.ID == "" {
		rec.ID = fmt.Sprintf("dep_%d", time.Now().UnixMilli())
	}
	if rec.DeployedAt.IsZero() {
		rec.DeployedAt = time.Now()
	}
	if rec.HealthStatus == "" {
		rec.HealthStatus = "unknown"
	}

	if err := s.save(rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// Get retrieves a single deployment by ID.
func (s *DeploymentStore) Get(id string) (*DeploymentRecord, error) {
	rec, err := s.load(id + ".jsonl")
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// UpdateHealth updates the health status (and error) for an existing deployment.
func (s *DeploymentStore) UpdateHealth(id, healthStatus string, lastChecked time.Time) error {
	rec, err := s.load(id + ".jsonl")
	if err != nil {
		return err
	}
	rec.HealthStatus = healthStatus
	rec.LastChecked = lastChecked
	return s.save(rec)
}

// Delete removes a deployment record.
func (s *DeploymentStore) Delete(id string) error {
	return os.Remove(s.path(id))
}

// --- JSONL I/O ---

func (s *DeploymentStore) load(filename string) (DeploymentRecord, error) {
	f, err := os.Open(filepath.Join(s.dir, filename))
	if err != nil {
		return DeploymentRecord{}, err
	}
	defer f.Close()

	var rec DeploymentRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var dl deplLine
		if err := json.Unmarshal([]byte(line), &dl); err != nil {
			continue
		}
		if dl.Type == "deployment" {
			rec = DeploymentRecord{
				ID:           dl.ID,
				ProjectName:  dl.ProjectName,
				ServerID:     dl.ServerID,
				ServerName:   dl.ServerName,
				Host:         dl.Host,
				Port:         dl.Port,
				Domain:       dl.Domain,
				Status:       dl.Status,
				HealthStatus: dl.HealthStatus,
				DeployedAt:   dl.DeployedAt,
				LastChecked:  dl.LastChecked,
				Error:        dl.Error,
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return DeploymentRecord{}, err
	}
	if rec.ID == "" {
		return DeploymentRecord{}, fmt.Errorf("no deployment record found in %s", filename)
	}
	return rec, nil
}

func (s *DeploymentStore) save(rec DeploymentRecord) error {
	f, err := os.Create(s.path(rec.ID))
	if err != nil {
		return fmt.Errorf("create deployment file: %w", err)
	}
	defer f.Close()

	dl := deplLine{
		Type:         "deployment",
		ID:           rec.ID,
		ProjectName:  rec.ProjectName,
		ServerID:     rec.ServerID,
		ServerName:   rec.ServerName,
		Host:         rec.Host,
		Port:         rec.Port,
		Domain:       rec.Domain,
		Status:       rec.Status,
		HealthStatus: rec.HealthStatus,
		DeployedAt:   rec.DeployedAt,
		LastChecked:  rec.LastChecked,
		Error:        rec.Error,
	}
	enc := json.NewEncoder(f)
	return enc.Encode(dl)
}
