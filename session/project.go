package session

import (
	"fmt"
	"strings"
	"time"
)

// Project represents a workspace containing sessions and groups
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Color     string    `json:"color,omitempty"`
}

// ProjectsData contains the list of projects and metadata
type ProjectsData struct {
	Projects    []*Project `json:"projects"`
	LastProject string     `json:"last_project,omitempty"`
}

// NewProject creates a new project with the given name
func NewProject(name string) *Project {
	id := generateProjectID(name)
	return &Project{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now(),
	}
}

// generateProjectID creates a sanitized ID from the project name
func generateProjectID(name string) string {
	sanitized := strings.ToLower(name)
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = strings.ReplaceAll(sanitized, "_", "-")
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("proj_%s_%d", sanitized, timestamp)
}

// GetSessionCount returns the number of sessions in a project
// This requires loading the project's sessions file
func (s *Storage) GetProjectSessionCount(projectID string) int {
	// Temporarily switch to project and count
	originalProject := s.projectID
	s.SetActiveProject(projectID)
	instances, _, _, _ := s.LoadAllWithSettings()
	s.SetActiveProject(originalProject)

	return len(instances)
}
