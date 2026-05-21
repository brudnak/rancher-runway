package test

import (
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type TerraformOutputs struct {
	ServerCount           int
	ServerIPs             []string
	ServerPrivateIPs      []string
	Server1IP             string
	Server2IP             string
	Server3IP             string
	Server4IP             string
	Server5IP             string
	Server1PrivateIP      string
	Server2PrivateIP      string
	Server3PrivateIP      string
	Server4PrivateIP      string
	Server5PrivateIP      string
	LoadBalancerDNS       string
	RancherURL            string
	GPUWorkerIP           string
	GPUWorkerPrivateIP    string
	GPUWorkerInstanceType string
	GPUWorkerAMI          string
	GPUWorkerSubnetID     string
}

type RancherResolvedPlan struct {
	Mode                   string
	RequestedVersion       string
	RequestedDistro        string
	BuildType              string
	ResolvedDistro         string
	ChartRepoAlias         string
	ChartVersion           string
	RancherImage           string
	RancherImageTag        string
	AgentImage             string
	UseRancherImageFields  bool
	CompatibilityBaseline  string
	SupportMatrixURL       string
	RecommendedRKE2Version string
	InstallerSHA256        string
	RecommendedK3SVersion  string
	K3SInstallerSHA256     string
	K3SAirgapImageSHA256   string
	HelmCommands           []string
	Explanation            []string
}

type helmSearchResult struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	AppVersion  string `json:"app_version"`
	Description string `json:"description"`
}

type cleanupCostEstimate struct {
	Region              string
	TotalRuntimeHours   float64
	InstanceCount       int
	InstanceType        string
	VolumeCount         int
	VolumeType          string
	VolumeSizeGiB       int32
	EC2HourlyRateUSD    float64
	EBSMonthlyRateUSD   float64
	EstimatedEC2CostUSD float64
	EstimatedEBSCostUSD float64
	DBInstanceCount     int
	DBInstanceClass     string
	RDSHourlyRateUSD    float64
	EstimatedRDSCostUSD float64
	LoadBalancerCount   int
	LoadBalancerType    string
	LBHourlyRateUSD     float64
	EstimatedLBCostUSD  float64
}

type resolvedChartMatch struct {
	repoAlias             string
	chartVersion          string
	compatibilityBaseline string
	matchRank             int
}

var (
	ssmClient   *ssm.Client
	ec2Client   *ec2.Client
	rdsClient   *rds.Client
	elbv2Client *elasticloadbalancingv2.Client
)
