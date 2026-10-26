package client

import "time"

const (
	ProcessStatusFinished      = "FINISHED"
	ProcessStatusFailed        = "FAILED"
	ProcessStatusCanceled      = "CANCELED"
	ProcessStatusPending       = "PENDING"
	ProcessStatusRunning       = "RUNNING"
	ServiceStatusActive        = "ACTIVE"
	ServiceStatusStopped       = "STOPPED"
	ServiceStatusReadyToDeploy = "READY_TO_DEPLOY"
	ProjectStatusActive        = "ACTIVE"
	AppVersionStatusActive     = "ACTIVE"
)

type PageQuery struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}
type ProjectQuery struct {
	PageQuery
	NameContains string   `json:"nameContains,omitempty"`
	Statuses     []string `json:"statuses,omitempty"`
}
type ServiceQuery struct {
	PageQuery
	NameContains string   `json:"nameContains,omitempty"`
	Statuses     []string `json:"statuses,omitempty"`
}
type UserDataQuery struct {
	PageQuery
	KeyContains string `json:"keyContains,omitempty"`
}
type Page[T any] struct {
	Items                []T
	Total, Offset, Limit int
}
type Organization struct{ ID, Name, MembershipID, RoleCode string }
type Project struct {
	ID                      string    `json:"id"`
	ClientID                string    `json:"clientId"`
	Name                    string    `json:"name"`
	Status                  string    `json:"status"`
	Mode                    string    `json:"mode"`
	Description             *string   `json:"description"`
	TagList                 []string  `json:"tagList"`
	PublicIPv4Shared        bool      `json:"publicIpV4Shared"`
	PublicIPv4              *string   `json:"publicIpV4"`
	PublicIPv6              *string   `json:"publicIpV6"`
	PublicZone              string    `json:"publicZone"`
	ZeropsSubdomainHost     string    `json:"zeropsSubdomainHost"`
	LogForwardingType       *string   `json:"logForwardingType"`
	Created                 time.Time `json:"created"`
	PrimaryInstanceLocation Location  `json:"primaryInstanceLocation"`
}
type ProjectCreate struct {
	Name             string            `json:"name"`
	Description      *string           `json:"description"`
	Mode             *string           `json:"mode"`
	TagList          []string          `json:"tagList"`
	UserRoles        []ProjectUserRole `json:"userRoles"`
	EnvVariables     []EnvWrite        `json:"envVariables"`
	EnvSecrets       []EnvWrite        `json:"envSecrets"`
	PublicIPv4Shared bool              `json:"publicIpV4Shared"`
	EnvIsolation     *string           `json:"envIsolation"`
	SSHIsolation     *string           `json:"sshIsolation"`
	MaxCreditLimit   *float64          `json:"maxCreditLimit"`
	Location         *string           `json:"location"`
}
type ProjectUserRole struct {
	UserID   string `json:"userId"`
	RoleCode string `json:"roleCode"`
}
type ProjectUpdate struct {
	Name             string   `json:"name"`
	Description      *string  `json:"description"`
	TagList          []string `json:"tagList"`
	PublicIPv4Shared bool     `json:"publicIpV4Shared"`
	MaxCreditLimit   *float64 `json:"maxCreditLimit"`
}
type Env struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"projectId"`
	ServiceStackID string    `json:"serviceStackId"`
	Key            string    `json:"key"`
	Content        string    `json:"content"`
	Sensitive      bool      `json:"sensitive"`
	Type           string    `json:"type"`
	Editable       bool      `json:"editable"`
	Internal       bool      `json:"internal"`
	Created        time.Time `json:"created"`
}
type EnvWrite struct {
	Key       string `json:"key"`
	Content   string `json:"content"`
	Sensitive bool   `json:"sensitive"`
}
type Service struct {
	ID                   string          `json:"id"`
	ProjectID            string          `json:"projectId"`
	Name                 string          `json:"name"`
	Status               string          `json:"status"`
	Base                 string          `json:"base"`
	ServiceStackTypeID   string          `json:"serviceStackTypeId"`
	SubdomainAccess      bool            `json:"subdomainAccess"`
	CDNEnabled           *bool           `json:"cdnEnabled"`
	IsSystem             bool            `json:"isSystem"`
	Created              time.Time       `json:"created"`
	CustomAutoscaling    *Autoscaling    `json:"customAutoscaling"`
	CurrentAutoscaling   *Autoscaling    `json:"currentAutoscaling"`
	AutoscalingProfileID *string         `json:"autoscalingProfileId"`
	Ports                []Port          `json:"ports"`
	ActiveAppVersion     *AppVersion     `json:"activeAppVersion"`
	ActiveAppVersionID   string          `json:"-"`
	ServiceTypeCategory  string          `json:"-"`
	ServiceStackTypeInfo ServiceTypeInfo `json:"serviceStackTypeInfo"`
	UserData             []Env           `json:"userData"`
}
type ServiceTypeInfo struct {
	Category    string `json:"serviceStackTypeCategory"`
	Name        string `json:"serviceStackTypeName"`
	VersionName string `json:"serviceStackTypeVersionName"`
}
type ServiceCreate struct {
	Name                        string         `json:"name"`
	ServiceStackVersionName     string         `json:"serviceStackVersionName"`
	StartWithoutCode            *bool          `json:"startWithoutCode"`
	CustomAutoscaling           *Autoscaling   `json:"customAutoscaling"`
	AutoscalingProfileID        *string        `json:"autoscalingProfileId"`
	AutoscalingProfileOverrides map[string]any `json:"autoscalingProfileOverrides"`
	UserData                    []EnvWrite     `json:"userData"`
	EnvVarsFile                 *string        `json:"envVarsFile"`
	EnvSecretsFile              *string        `json:"envSecretsFile"`
	BuildFromGit                *string        `json:"buildFromGit"`
	ZeropsSetup                 *string        `json:"zeropsSetup"`
	ZeropsYaml                  *string        `json:"zeropsYaml"`
	EnableSubdomainAccess       *bool          `json:"enableSubdomainAccess"`
	EnvIsolation                *string        `json:"envIsolation"`
	SSHIsolation                *string        `json:"sshIsolation"`
	CDNEnabled                  *bool          `json:"cdnEnabled"`
	Location                    *string        `json:"location"`
}
type Autoscaling struct {
	Vertical   *VerticalAutoscaling   `json:"verticalAutoscaling"`
	Horizontal *HorizontalAutoscaling `json:"horizontalAutoscaling"`
}
type VerticalAutoscaling struct {
	MaxResource       *ScalingResource        `json:"maxResource"`
	MinResource       *ScalingResource        `json:"minResource"`
	MinFreeResource   *ScalingMinFreeResource `json:"minFreeResource"`
	CPUMode           *string                 `json:"cpuMode"`
	StartCPUCoreCount *int                    `json:"startCpuCoreCount"`
	SwapEnabled       *bool                   `json:"swapEnabled"`
}
type HorizontalAutoscaling struct {
	MinContainerCount *int `json:"minContainerCount"`
	MaxContainerCount *int `json:"maxContainerCount"`
}
type ScalingResource struct {
	CPUCoreCount *int     `json:"cpuCoreCount"`
	MemoryGBytes *float64 `json:"memoryGBytes"`
	DiskGBytes   *float64 `json:"diskGBytes"`
}
type ScalingMinFreeResource struct {
	CPUCoreCount   *float64 `json:"cpuCoreCount"`
	CPUCorePercent *float64 `json:"cpuCorePercent"`
	MemoryGBytes   *float64 `json:"memoryGBytes"`
	MemoryPercent  *float64 `json:"memoryPercent"`
}
type AutoscalingWrite struct {
	CustomAutoscaling           *Autoscaling   `json:"customAutoscaling"`
	AutoscalingProfileID        *string        `json:"autoscalingProfileId"`
	AutoscalingProfileOverrides map[string]any `json:"autoscalingProfileOverrides"`
}
type Port struct {
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Scheme      string `json:"scheme"`
	Description string `json:"description"`
	HTTPRouting *bool  `json:"httpRouting"`
	PortRouting *bool  `json:"portRouting"`
}
type StorageCreate struct {
	Name       string  `json:"name"`
	DiskGBytes int     `json:"diskGBytes"`
	Policy     *string `json:"policy"`
	RawPolicy  *string `json:"rawPolicy"`
	CDNEnabled *bool   `json:"cdnEnabled"`
}
type StorageWrite struct {
	DiskGBytes int     `json:"diskGBytes"`
	Policy     *string `json:"policy"`
	RawPolicy  *string `json:"rawPolicy"`
	CDNEnabled *bool   `json:"cdnEnabled"`
}
type StorageInfo struct {
	DiskGBytesUsed float64 `json:"diskGBytesUsed"`
	Objects        int64   `json:"objects"`
	RawPolicy      string  `json:"rawPolicy"`
}
type HTTPRouteWrite struct {
	SSLEnabled bool           `json:"sslEnabled"`
	CDNEnabled *bool          `json:"cdnEnabled"`
	Domains    []string       `json:"domains"`
	Locations  []HTTPLocation `json:"locations"`
}
type HTTPLocation struct {
	Path           string         `json:"path"`
	Port           int            `json:"port"`
	ServiceStackID string         `json:"serviceStackId"`
	Config         map[string]any `json:"config"`
}
type HTTPRoute struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"projectId"`
	SSLEnabled   bool           `json:"sslEnabled"`
	CDNEnabled   bool           `json:"cdnEnabled"`
	Domains      []HTTPDomain   `json:"domains"`
	Locations    []HTTPLocation `json:"locations"`
	IsSynced     bool           `json:"isSynced"`
	IsEditable   bool           `json:"isEditable"`
	DeleteOnSync bool           `json:"deleteOnSync"`
}
type HTTPDomain struct {
	DomainName           string  `json:"domainName"`
	SSLStatus            string  `json:"sslStatus"`
	DNSCheckStatus       string  `json:"dnsCheckStatus"`
	CDNStatus            string  `json:"cdnStatus"`
	SSLInstallationError *string `json:"sslCertificateInstallationError"`
}
type PortRouteWrite struct {
	PublicIPType      string   `json:"publicIpType"`
	PublicPort        int      `json:"publicPort"`
	InternalPort      int      `json:"internalPort"`
	InternalProtocol  string   `json:"internalProtocol"`
	FirewallPolicy    string   `json:"firewallPolicy"`
	FirewallIPRanges  []string `json:"firewallIpRanges"`
	FirewallAllowMyIP bool     `json:"firewallAllowMyIp"`
}
type PortRoute struct {
	PortRouteWrite
	ID             string `json:"id"`
	ProjectID      string `json:"projectId"`
	ServiceStackID string `json:"serviceStackId"`
	IsSynced       bool   `json:"isSynced"`
	DeleteOnSync   bool   `json:"deleteOnSync"`
}
type Process struct {
	ID             string        `json:"id"`
	ActionName     string        `json:"actionName"`
	Status         string        `json:"status"`
	ProjectID      string        `json:"projectId"`
	ServiceStackID string        `json:"serviceStackId"`
	Created        *time.Time    `json:"created"`
	Started        *time.Time    `json:"started"`
	Finished       *time.Time    `json:"finished"`
	Error          *ProcessError `json:"error"`
	AppVersionID   string        `json:"appVersionId"`
	AppVersion     *AppVersion   `json:"appVersion"`
}
type ProcessError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Meta    any    `json:"meta"`
}
type ServiceProcess struct {
	Service
	Process *Process `json:"process"`
}
type AppVersion struct {
	ID                string         `json:"id"`
	ServiceStackID    string         `json:"serviceStackId"`
	Name              string         `json:"name"`
	Status            string         `json:"status"`
	Source            string         `json:"source"`
	GitBranch         string         `json:"gitBranch"`
	Created           time.Time      `json:"created"`
	Sequence          int            `json:"sequence"`
	Build             map[string]any `json:"build"`
	PublicGitSource   map[string]any `json:"publicGitSource"`
	GithubIntegration map[string]any `json:"githubIntegration"`
	GitlabIntegration map[string]any `json:"gitlabIntegration"`
}
type UploadVersion struct {
	AppVersion
	UploadURL string `json:"uploadUrl"`
}
type DeployRequest struct {
	ZeropsYaml string `json:"zeropsYaml"`
	Setup      string `json:"zeropsYamlSetup"`
}
type PipelineRequest struct {
	BuildFromGit string `json:"buildFromGit"`
	Setup        string `json:"zeropsSetup"`
	ZeropsYaml   string `json:"zeropsYaml"`
}
type GitPipelineRequest = PipelineRequest
type PipelineProcess struct {
	Process      *Process    `json:"process"`
	AppVersion   *AppVersion `json:"appVersion"`
	AppVersionID string      `json:"appVersionId"`
}
type Settings struct {
	Locations    []Location    `json:"locationList"`
	ServiceTypes []ServiceType `json:"serviceStackList"`
}
type Location struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	PingURL string `json:"pingUrl"`
}
type ServiceType struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Category         string               `json:"category"`
	IsRuntime        bool                 `json:"isRuntime"`
	IsManaged        bool                 `json:"isManaged"`
	Modes            []string             `json:"modeList"`
	OperatingSystems []string             `json:"osList"`
	Versions         []ServiceTypeVersion `json:"serviceStackTypeVersionList"`
}
type ServiceTypeVersion struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Status             string            `json:"status"`
	VersionNumber      string            `json:"versionNumber"`
	ExactVersionNumber *string           `json:"exactVersionNumber"`
	Config             ServiceTypeConfig `json:"config"`
}
type ServiceTypeConfig struct {
	DefaultAutoscaling  *Autoscaling         `json:"defaultAutoscaling"`
	VerticalAutoscaling *VerticalAutoscaling `json:"verticalAutoscaling"`
}
type Region struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	IsDefault bool   `json:"isDefault"`
}
