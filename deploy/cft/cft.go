// Package cft provides utilities to deploy CFT resources.
package cft

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sort"
)

const deploymentName = "managed-data-protect-toolkit"

// Config represents a (partial) representation of a projects YAML file.
// Only the required fields are present. See project_config.yaml.schema for details.
// TODO: move config to its own package
// TODO: support new generated_fields
type Config struct {
	Overall struct {
		Domain         string   `json:"domain"`
		OrganizationID string   `json:"organization_id"`
		FolderID       string   `json:"folder_id"`
		AllowedAPIs    []string `json:"allowed_apis"`
	} `json:"overall"`
	AuditLogsProject *Project `json:"audit_logs_project"`
	Forseti          *struct {
		Project         *Project `json:"project"`
		GeneratedFields struct {
			ServiceAccount string `json:"service_account"`
			ServerBucket   string `json:"server_bucket"`
		} `json:"generated_fields"`
	} `json:"forseti"`
	Projects []*Project `json:"projects"`
}

// Project defines a single project's configuration.
type Project struct {
	ID                  string   `json:"project_id"`
	OwnersGroup         string   `json:"owners_group"`
	AuditorsGroup       string   `json:"auditors_group"`
	EditorsGroup        string   `json:"editors_group"`
	DataReadWriteGroups []string `json:"data_readwrite_groups"`
	DataReadOnlyGroups  []string `json:"data_readonly_groups"`
	EnabledAPIs         []string `json:"enabled_apis"`

	// Note: typically only one resource in the struct is set at one time.
	// Go does not have the concept of "one-of", so despite only one of these resources
	// typically being non-empty, we still check them all. The one-of check is done by the schema.
	Resources []*struct {
		// The following structs are embedded so the json parser skips directly to their fields.
		BigqueryDatasetPair
		FirewallPair
		GCEInstancePair
		GCSBucketPair
		GKEClusterPair
		IAMCustomRolePair
		IAMPolicyPair
		PubsubPair

		// TODO: make this behave more like standard deployment manager resources
		GKEWorkload json.RawMessage `json:"gke_workload"`
	} `json:"resources"`

	AuditLogs *struct {
		LogsGCSBucket *struct {
			Name     string `json:"name"`
			Location string `json:"location"`
		} `json:"logs_gcs_bucket"`

		LogsBigqueryDataset struct {
			Name     string `json:"name"`
			Location string `json:"location"`
		} `json:"logs_bigquery_dataset"`
	} `json:"audit_logs"`

	GeneratedFields struct {
		ProjectNumber         string `json:"project_number"`
		LogSinkServiceAccount string `json:"log_sink_service_account"`
		GCEInstanceInfo       []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"gce_instance_info"`
	} `json:"generated_fields"`
}

// BigqueryDatasetPair pairs a raw dataset with its parsed version.
type BigqueryDatasetPair struct {
	Raw    json.RawMessage `json:"bq_dataset"`
	Parsed BigqueryDataset `json:"-"`
}

// FirewallPair pairs a raw firewall with its parsed version.
type FirewallPair struct {
	Raw    json.RawMessage `json:"firewall"`
	Parsed DefaultResource `json:"-"`
}

// GCEInstancePair pairs a raw instance with its parsed version.
type GCEInstancePair struct {
	Raw    json.RawMessage `json:"gce_instance"`
	Parsed GCEInstance     `json:"-"`
}

// GCSBucketPair pairs a raw bucket with its parsed version.
type GCSBucketPair struct {
	Raw    json.RawMessage `json:"gcs_bucket"`
	Parsed GCSBucket       `json:"-"`
}

// GKEClusterPair pairs a raw cluster with its parsed version.
type GKEClusterPair struct {
	Raw    json.RawMessage `json:"gke_cluster"`
	Parsed GKECluster      `json:"-"`
}

// IAMCustomRolePair pairs a raw custom role with its parsed version.
type IAMCustomRolePair struct {
	Raw    json.RawMessage `json:"iam_custom_role"`
	Parsed IAMCustomRole   `json:"-"`
}

// IAMPolicyPair pairs a raw iam policy with its parsed version.
type IAMPolicyPair struct {
	Raw    json.RawMessage `json:"iam_policy"`
	Parsed IAMPolicy       `json:"-"`
}

// PubsubPair pairs a raw pubsub with its parsed version.
type PubsubPair struct {
	Raw    json.RawMessage `json:"pubsub"`
	Parsed Pubsub          `json:"-"`
}

// Init initializes the config and all its projects.
func (c *Config) Init() error {
	for _, p := range c.AllProjects() {
		if err := p.Init(); err != nil {
			return fmt.Errorf("failed to init project %q: %v", p.ID, err)
		}
	}
	return nil
}

// AllProjects returns all projects in this config.
// This includes Audit, Forseti and all data hosting projects.
func (c *Config) AllProjects() []*Project {
	ps := make([]*Project, 0, len(c.Projects))
	if c.AuditLogsProject != nil {
		ps = append(ps, c.AuditLogsProject)
	}
	if c.Forseti != nil {
		ps = append(ps, c.Forseti.Project)
	}
	ps = append(ps, c.Projects...)
	return ps
}

// ProjectForAuditLogs is a helper function to get the audit logs project for the given project.
// Return the remote audit logs project if it exists, else return the project itself (to store audit logs locally).
func (c *Config) ProjectForAuditLogs(p *Project) *Project {
	if c.AuditLogsProject != nil {
		return c.AuditLogsProject
	}
	return p
}

// Init initializes a project and all its resources.
func (p *Project) Init() error {
	if p.AuditLogs.LogsBigqueryDataset.Name == "" {
		p.AuditLogs.LogsBigqueryDataset.Name = "audit_logs"
	}

	if p.AuditLogs.LogsGCSBucket != nil && p.AuditLogs.LogsGCSBucket.Name == "" {
		p.AuditLogs.LogsGCSBucket.Name = p.ID + "-logs"
	}

	for _, pair := range p.resourcePairs() {
		if err := json.Unmarshal(pair.raw, pair.parsed); err != nil {
			return err
		}
		if err := pair.parsed.Init(p); err != nil {
			return fmt.Errorf("failed to init: %v, %+v", err, pair.parsed)
		}
	}
	return nil
}

func (p *Project) resourcePairs() []resourcePair {
	var pairs []resourcePair
	appendPair := func(raw json.RawMessage, parsed parsedResource) {
		if len(raw) > 0 {
			pairs = append(pairs, resourcePair{raw, parsed})
		}
	}
	appendDefaultResPair := func(raw json.RawMessage, parsed *DefaultResource, path string) {
		parsed.templatePath = path
		appendPair(raw, parsed)
	}
	for _, res := range p.Resources {
		appendPair(res.BigqueryDatasetPair.Raw, &res.BigqueryDatasetPair.Parsed)
		appendDefaultResPair(res.FirewallPair.Raw, &res.FirewallPair.Parsed, "deploy/cft/templates/firewall/firewall.py")
		appendPair(res.GCEInstancePair.Raw, &res.GCEInstancePair.Parsed)
		appendPair(res.GCSBucketPair.Raw, &res.GCSBucketPair.Parsed)
		appendPair(res.GKEClusterPair.Raw, &res.GKEClusterPair.Parsed)
		appendPair(res.IAMCustomRolePair.Raw, &res.IAMCustomRolePair.Parsed)
		appendPair(res.IAMPolicyPair.Raw, &res.IAMPolicyPair.Parsed)
		appendPair(res.PubsubPair.Raw, &res.PubsubPair.Parsed)
	}
	return pairs
}

// ResourcesByType arranges resources in the project into slices by their type.
type ResourcesByType struct {
	BigqueryDatasets []*BigqueryDataset
	GCSBuckets       []*GCSBucket
	GCEInstances     []*GCEInstance
	IAMPolicies      []*IAMPolicy
}

// ResourcesByType gets resources in the project as slices arranged by their type.
func (p *Project) ResourcesByType() *ResourcesByType {
	rs := &ResourcesByType{}
	for _, r := range p.Resources {
		switch {
		case len(r.BigqueryDatasetPair.Raw) > 0:
			rs.BigqueryDatasets = append(rs.BigqueryDatasets, &r.BigqueryDatasetPair.Parsed)
		case len(r.GCSBucketPair.Raw) > 0:
			rs.GCSBuckets = append(rs.GCSBuckets, &r.GCSBucketPair.Parsed)
		case len(r.GCEInstancePair.Raw) > 0:
			rs.GCEInstances = append(rs.GCEInstances, &r.GCEInstancePair.Parsed)
		case len(r.IAMPolicyPair.Raw) > 0:
			rs.IAMPolicies = append(rs.IAMPolicies, &r.IAMPolicyPair.Parsed)
		}
	}
	return rs
}

// InstanceID returns the ID of the instance with the given name.
func (p *Project) InstanceID(name string) (string, error) {
	for _, info := range p.GeneratedFields.GCEInstanceInfo {
		if info.Name == name {
			return info.ID, nil
		}
	}
	return "", fmt.Errorf("info for instance %q not found in generated_fields", name)
}

// parsedResource is an interface that must be implemented by all concrete resource implementations.
type parsedResource interface {
	Init(*Project) error
	Name() string
}

// deploymentManagerTyper should be implemented by resources that are natively supported by DM.
// Use this if there is no suitable CFT template for a resource and a custom template is not needed.
// See https://cloud.google.com/deployment-manager/docs/configuration/supported-resource-types for valid types.
type deploymentManagerTyper interface {
	DeploymentManagerType() string
}

// deploymentManagerPather should be implemented by resources that use a DM template to deploy.
// Use this if the resource wraps a CFT or custom template.
type deploymentManagerPather interface {
	TemplatePath() string
}

// depender is the interface that defines a method to get dependent resources.
type depender interface {
	DependentResources(*Project) ([]parsedResource, error)
}

// Deploy deploys the CFT resources in the project.
func Deploy(project *Project) error {
	pairs := project.resourcePairs()
	if len(pairs) == 0 {
		log.Println("No resources to deploy.")
		return nil
	}

	deployment, err := getDeployment(project, pairs)
	if err != nil {
		return err
	}
	if err := createOrUpdateDeployment(deploymentName, deployment, project.ID); err != nil {
		return fmt.Errorf("failed to deploy deployment manager resources: %v", err)
	}

	if err := deployGKEWorkloads(project); err != nil {
		return fmt.Errorf("failed to deploy GKE workloads: %v", err)
	}

	return nil
}

func getDeployment(project *Project, pairs []resourcePair) (*Deployment, error) {
	deployment := &Deployment{}

	allImports := make(map[string]bool)

	for _, pair := range pairs {
		resources, importSet, err := getDeploymentResourcesAndImports(pair, project)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource and imports for %q: %v", pair.parsed.Name(), err)
		}
		deployment.Resources = append(deployment.Resources, resources...)

		var imports []string
		for imp := range importSet {
			imports = append(imports, imp)
		}
		sort.Strings(imports) // for stable ordering

		for _, imp := range imports {
			if !allImports[imp] {
				deployment.Imports = append(deployment.Imports, &Import{Path: imp})
			}
			allImports[imp] = true
		}
	}

	return deployment, nil
}

// getDeploymentResourcesAndImports gets the deploment resources and imports for the given resource pair.
// It also recursively adds the resoures and imports of any dependent resources.
func getDeploymentResourcesAndImports(pair resourcePair, project *Project) (resources []*Resource, importSet map[string]bool, err error) {
	importSet = make(map[string]bool)

	var typ string
	if typer, ok := pair.parsed.(deploymentManagerTyper); ok {
		typ = typer.DeploymentManagerType()
	} else if pather, ok := pair.parsed.(deploymentManagerPather); ok {
		var err error
		typ, err = filepath.Abs(pather.TemplatePath())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get absolute path for %q: %v", pather.TemplatePath(), err)
		}
		importSet[typ] = true
	} else {
		return nil, nil, fmt.Errorf("failed to get type of %+v", pair.parsed)
	}

	merged, err := pair.MergedPropertiesMap()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to merge raw map with parsed: %v", err)
	}

	resources = []*Resource{{
		Name:       pair.parsed.Name(),
		Type:       typ,
		Properties: merged,
	}}

	dr, ok := pair.parsed.(depender)
	if !ok { // doesn't implement dependent resources method so has no dependent resources
		return resources, importSet, nil
	}

	dependencies, err := dr.DependentResources(project)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get dependent resources for %q: %v", pair.parsed.Name(), err)
	}

	for _, dep := range dependencies {
		depPair := resourcePair{parsed: dep}
		depResources, depImports, err := getDeploymentResourcesAndImports(depPair, project)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get resources and imports for %q: %v", dep.Name(), err)
		}

		for _, res := range depResources {
			if res.Metadata == nil {
				res.Metadata = &Metadata{}
			}
			res.Metadata.DependsOn = append(res.Metadata.DependsOn, pair.parsed.Name())
		}
		resources = append(resources, depResources...)

		for imp := range depImports {
			importSet[imp] = true
		}
	}
	return resources, importSet, nil
}
