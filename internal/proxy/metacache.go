package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// FieldMeta represents metadata for a single field/column
type FieldMeta struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// TableMeta represents metadata for a single NocoDB table
type TableMeta struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	TableName string      `json:"table_name"`
	Columns   []FieldMeta `json:"columns,omitempty"`
	Fields    []FieldMeta `json:"fields,omitempty"`
}

// TablesResponse represents the response from NocoDB meta API
type TablesResponse struct {
	List []TableMeta `json:"list"`
}

// MetaCache maintains a thread-safe cache of table name to ID mappings
type MetaCache struct {
	mu                sync.RWMutex
	tableByName       map[string]string            // lowercase friendly title -> table ID
	fieldsByTable     map[string]map[string]string // table ID -> (lowercase field name -> field ID)
	linkFieldsByTable map[string]map[string]string // table ID -> (lowercase link field name -> field ID)
	metaBaseURL       string                       // e.g. http://100.103.198.65:8090/api/v2/
	BaseID            string                       // NocoDB base ID
	token             string                       // NOCODB_TOKEN
	httpClient        *http.Client
	lastLoadedAt      time.Time
	refreshInterval   time.Duration
}

// NewMetaCache creates a new MetaCache instance
func NewMetaCache(metaBaseURL, baseID, token string) *MetaCache {
	return &MetaCache{
		tableByName:       make(map[string]string),
		fieldsByTable:     make(map[string]map[string]string),
		linkFieldsByTable: make(map[string]map[string]string),
		metaBaseURL:       strings.TrimRight(metaBaseURL, "/") + "/",
		BaseID:            baseID,
		token:             token,
		httpClient:        &http.Client{Timeout: 10 * time.Second},
		refreshInterval:   10 * time.Minute,
	}
}

// fetchTableDetails fetches detailed metadata for a specific table including fields
func (m *MetaCache) fetchTableDetails(tableID string) (*TableMeta, error) {
	// Construct v2 API URL for table details
	url := fmt.Sprintf("%smeta/tables/%s", m.metaBaseURL, tableID)

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create table details request: %w", err)
	}

	// Add authentication header
	req.Header.Set("xc-token", m.token)

	// Execute request
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch table details: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("table details API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read table details response: %w", err)
	}

	var tableMeta TableMeta
	if err := json.Unmarshal(body, &tableMeta); err != nil {
		return nil, fmt.Errorf("failed to parse table details JSON: %w", err)
	}

	return &tableMeta, nil
}

// Refresh fetches table metadata from NocoDB and updates the cache
func (m *MetaCache) Refresh() error {
	log.Printf("[META] Fetching table metadata from NocoDB...")

	// Build the metadata API URL
	url := fmt.Sprintf("%smeta/bases/%s/tables", m.metaBaseURL, m.BaseID)
	log.Printf("[META] Metadata URL: %s", url)

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create metadata request: %w", err)
	}

	// Add authentication header
	req.Header.Set("xc-token", m.token)

	// Execute request
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("metadata API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read metadata response: %w", err)
	}

	var tablesResp TablesResponse
	if err := json.Unmarshal(body, &tablesResp); err != nil {
		return fmt.Errorf("failed to parse metadata JSON: %w", err)
	}

	// Build new mapping
	newMapping := make(map[string]string)
	newFieldMappings := make(map[string]map[string]string)
	newLinkFieldMappings := make(map[string]map[string]string)

	for _, table := range tablesResp.List {
		// Map both lowercase title and table_name to ID
		if table.Title != "" {
			newMapping[strings.ToLower(table.Title)] = table.ID
			log.Printf("[META] Mapped table '%s' -> '%s'", table.Title, table.ID)
		}
		if table.TableName != "" && table.TableName != table.Title {
			newMapping[strings.ToLower(table.TableName)] = table.ID
			log.Printf("[META] Mapped table '%s' -> '%s'", table.TableName, table.ID)
		}

		// Map fields for this table
		if len(table.Columns) > 0 {
			fieldMap := make(map[string]string)
			for _, field := range table.Columns {
				if field.Title != "" {
					fieldMap[strings.ToLower(field.Title)] = field.ID
					log.Printf("[META] Mapped field '%s.%s' -> '%s'", table.Title, field.Title, field.ID)
				}
			}
			newFieldMappings[table.ID] = fieldMap
		}

		// Fetch detailed table metadata to get link fields
		log.Printf("[META] Fetching field metadata for table '%s' (%s)...", table.Title, table.ID)
		tableDetails, err := m.fetchTableDetails(table.ID)
		if err != nil {
			log.Printf("[META WARNING] Failed to fetch field details for table '%s': %v", table.Title, err)
			continue
		}

		// Extract link fields from the detailed metadata
		linkFieldMap := make(map[string]string)
		for _, field := range tableDetails.Fields {
			if field.Type == "Links" || field.Type == "LinkToAnotherRecord" {
				if field.Title != "" {
					linkFieldMap[strings.ToLower(field.Title)] = field.ID
					log.Printf("[META] ✓ Found link field '%s.%s' (ID: %s, Type: %s)", table.Title, field.Title, field.ID, field.Type)
				}
			}
		}

		if len(linkFieldMap) > 0 {
			newLinkFieldMappings[table.ID] = linkFieldMap
			log.Printf("[META] Cached %d link field(s) for table '%s'", len(linkFieldMap), table.Title)
		}
	}

	// Count total link fields
	totalLinkFields := 0
	for _, linkFields := range newLinkFieldMappings {
		totalLinkFields += len(linkFields)
	}

	// Update cache atomically
	m.mu.Lock()
	m.tableByName = newMapping
	m.fieldsByTable = newFieldMappings
	m.linkFieldsByTable = newLinkFieldMappings
	m.lastLoadedAt = time.Now()
	m.mu.Unlock()

	log.Printf("[META] ✅ Successfully loaded %d tables and %d link field mappings", len(tablesResp.List), totalLinkFields)
	return nil
}

// Resolve looks up a table ID by its friendly name
func (m *MetaCache) Resolve(name string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.tableByName == nil {
		return "", false
	}

	id, ok := m.tableByName[strings.ToLower(name)]
	return id, ok
}

// ResolveTable looks up a table ID by its friendly name (alias for Resolve)
func (m *MetaCache) ResolveTable(name string) (string, bool) {
	return m.Resolve(name)
}

// ResolveField looks up a field ID by its name within a specific table
func (m *MetaCache) ResolveField(tableID, fieldName string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.fieldsByTable == nil {
		return "", false
	}

	fieldMap, ok := m.fieldsByTable[tableID]
	if !ok {
		return "", false
	}

	fieldID, ok := fieldMap[strings.ToLower(fieldName)]
	return fieldID, ok
}

// ResolveLinkField looks up a link field ID by its name within a specific table
func (m *MetaCache) ResolveLinkField(tableID, fieldName string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.linkFieldsByTable == nil {
		log.Printf("[META DEBUG] linkFieldsByTable is nil")
		return "", false
	}

	linkFieldMap, ok := m.linkFieldsByTable[tableID]
	if !ok {
		log.Printf("[META DEBUG] No link fields found for table ID: %s", tableID)
		return "", false
	}

	fieldID, ok := linkFieldMap[strings.ToLower(fieldName)]
	if !ok {
		log.Printf("[META DEBUG] Link field '%s' not found in table %s", fieldName, tableID)
	}
	return fieldID, ok
}

// ShouldRefresh checks if the cache should be refreshed
func (m *MetaCache) ShouldRefresh() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lastLoadedAt.IsZero() {
		return true
	}

	return time.Since(m.lastLoadedAt) > m.refreshInterval
}

// GetLastRefreshTime returns when the cache was last refreshed
func (m *MetaCache) GetLastRefreshTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastLoadedAt
}

// LoadInitial performs an initial synchronous metadata fetch
func (m *MetaCache) LoadInitial() error {
	log.Printf("[META] Performing initial synchronous metadata load...")
	if err := m.Refresh(); err != nil {
		return fmt.Errorf("initial metadata load failed: %w", err)
	}
	log.Printf("[META] Initial metadata load complete: %d tables cached", m.GetTableCount())
	return nil
}

// StartAutoRefresh starts a background goroutine that periodically refreshes the cache
func (m *MetaCache) StartAutoRefresh() {
	go func() {
		log.Printf("[META] Starting auto-refresh goroutine (interval: %v)", m.refreshInterval)

		// Periodic refresh
		ticker := time.NewTicker(m.refreshInterval)
		defer ticker.Stop()

		for range ticker.C {
			log.Printf("[META] Auto-refreshing metadata cache...")
			if err := m.Refresh(); err != nil {
				log.Printf("[META ERROR] Auto-refresh failed: %v", err)
				// Don't crash - keep the old cache
			}
		}
	}()
}

// IsReady returns true if the cache has been loaded at least once
func (m *MetaCache) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.lastLoadedAt.IsZero()
}

// GetTableCount returns the number of cached table mappings
func (m *MetaCache) GetTableCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tableByName)
}

// GetLinkFieldTableCount returns the number of tables with cached link field mappings
func (m *MetaCache) GetLinkFieldTableCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.linkFieldsByTable)
}
