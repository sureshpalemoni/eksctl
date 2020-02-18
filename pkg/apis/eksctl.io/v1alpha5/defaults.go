package v1alpha5

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math"
	"os"
	"strconv"
	"strings"
)

// SetClusterConfigDefaults will set defaults for a given cluster
func SetClusterConfigDefaults(cfg *ClusterConfig) {
	if cfg.IAM == nil {
		cfg.IAM = &ClusterIAM{}
	}

	if cfg.IAM.WithOIDC == nil {
		cfg.IAM.WithOIDC = Disabled()
	}

	for _, sa := range cfg.IAM.ServiceAccounts {
		if sa.Namespace == "" {
			sa.Namespace = metav1.NamespaceDefault
		}
	}

	if cfg.HasClusterCloudWatchLogging() && len(cfg.CloudWatch.ClusterLogging.EnableTypes) == 1 {
		switch cfg.CloudWatch.ClusterLogging.EnableTypes[0] {
		case "all", "*":
			cfg.CloudWatch.ClusterLogging.EnableTypes = SupportedCloudWatchClusterLogTypes()
		}
	}
}

// SetNodeGroupDefaults will set defaults for a given nodegroup
func SetNodeGroupDefaults(ng *NodeGroup, meta *ClusterMeta) {
	if ng.InstanceType == "" {
		if HasMixedInstances(ng) {
			ng.InstanceType = "mixed"
		} else {
			ng.InstanceType = DefaultNodeType
		}
	}
	if ng.AMIFamily == "" {
		ng.AMIFamily = DefaultNodeImageFamily
	}

	if ng.SecurityGroups == nil {
		ng.SecurityGroups = &NodeGroupSGs{
			AttachIDs: []string{},
		}
	}
	if ng.SecurityGroups.WithLocal == nil {
		ng.SecurityGroups.WithLocal = Enabled()
	}
	if ng.SecurityGroups.WithShared == nil {
		ng.SecurityGroups.WithShared = Enabled()
	}

	if ng.SSH == nil {
		ng.SSH = &NodeGroupSSH{
			Allow: Disabled(),
		}
	}

	setSSHDefaults(ng.SSH)

	if !IsSetAndNonEmptyString(ng.VolumeType) {
		ng.VolumeType = &DefaultNodeVolumeType
	}

	if ng.IAM == nil {
		ng.IAM = &NodeGroupIAM{}
	}

	setIAMDefaults(ng.IAM)

	if ng.Labels == nil {
		ng.Labels = make(map[string]string)
	}
	setDefaultNodeLabels(ng.Labels, meta.Name, ng.Name)

	if ng.KubeletExtraConfig == nil {
		ng.KubeletExtraConfig = &InlineDocument{}
	}

	SetKubeletExtraConfigDefaults(ng, meta)
}

// SetManagedNodeGroupDefaults sets default values for a ManagedNodeGroup
func SetManagedNodeGroupDefaults(ng *ManagedNodeGroup, meta *ClusterMeta) {
	if ng.AMIFamily == "" {
		ng.AMIFamily = NodeImageFamilyAmazonLinux2
	}
	if ng.InstanceType == "" {
		ng.InstanceType = DefaultNodeType
	}
	if ng.ScalingConfig == nil {
		ng.ScalingConfig = &ScalingConfig{}
	}
	if ng.SSH == nil {
		ng.SSH = &NodeGroupSSH{
			Allow: Disabled(),
		}
	}
	setSSHDefaults(ng.SSH)

	if ng.IAM == nil {
		ng.IAM = &NodeGroupIAM{}
	}
	setIAMDefaults(ng.IAM)

	if ng.Labels == nil {
		ng.Labels = make(map[string]string)
	}
	setDefaultNodeLabels(ng.Labels, meta.Name, ng.Name)

	if ng.Tags == nil {
		ng.Tags = make(map[string]string)
	}
	ng.Tags[NodeGroupNameTag] = ng.Name
	ng.Tags[NodeGroupTypeTag] = string(NodeGroupTypeManaged)
}

func setIAMDefaults(iamConfig *NodeGroupIAM) {
	if iamConfig.WithAddonPolicies.ImageBuilder == nil {
		iamConfig.WithAddonPolicies.ImageBuilder = Disabled()
	}
	if iamConfig.WithAddonPolicies.AutoScaler == nil {
		iamConfig.WithAddonPolicies.AutoScaler = Disabled()
	}
	if iamConfig.WithAddonPolicies.ExternalDNS == nil {
		iamConfig.WithAddonPolicies.ExternalDNS = Disabled()
	}
	if iamConfig.WithAddonPolicies.CertManager == nil {
		iamConfig.WithAddonPolicies.CertManager = Disabled()
	}
	if iamConfig.WithAddonPolicies.ALBIngress == nil {
		iamConfig.WithAddonPolicies.ALBIngress = Disabled()
	}
	if iamConfig.WithAddonPolicies.XRay == nil {
		iamConfig.WithAddonPolicies.XRay = Disabled()
	}
	if iamConfig.WithAddonPolicies.CloudWatch == nil {
		iamConfig.WithAddonPolicies.CloudWatch = Disabled()
	}
	if iamConfig.WithAddonPolicies.EBS == nil {
		iamConfig.WithAddonPolicies.EBS = Disabled()
	}
	if iamConfig.WithAddonPolicies.FSX == nil {
		iamConfig.WithAddonPolicies.FSX = Disabled()
	}
	if iamConfig.WithAddonPolicies.EFS == nil {
		iamConfig.WithAddonPolicies.EFS = Disabled()
	}
}

func setSSHDefaults(sshConfig *NodeGroupSSH) {
	numSSHFlagsEnabled := countEnabledFields(
		sshConfig.PublicKeyName,
		sshConfig.PublicKeyPath,
		sshConfig.PublicKey)

	if numSSHFlagsEnabled == 0 {
		if IsEnabled(sshConfig.Allow) {
			sshConfig.PublicKeyPath = &DefaultNodeSSHPublicKeyPath
		} else {
			sshConfig.Allow = Disabled()
		}
	} else if !IsDisabled(sshConfig.Allow) {
		// Enable SSH if not explicitly disabled when passing an SSH key
		sshConfig.Allow = Enabled()
	}

}

func setDefaultNodeLabels(labels map[string]string, clusterName, nodeGroupName string) {
	labels[ClusterNameLabel] = clusterName
	labels[NodeGroupNameLabel] = nodeGroupName
}

type getRscDefaultFunc func(string, *ClusterMeta) (string, error)
type setRscDefaultFunc func(*NodeGroup, string, *ClusterMeta, getRscDefaultFunc) error

type rscParamSet struct {
	setFun  setRscDefaultFunc `json: "setFun,omitEmpty"`
	getFun  getRscDefaultFunc `json: "getFun,omitEmpty"`
	rscType string            `json: "rscType,omitEmpty"`
}

var rscParams = []rscParamSet{
	{setFun: setCpuReservationsDefaults, getFun: getCpuReservations, rscType: "cpu"},
	{setFun: setMemoryResevationDefaults, getFun: getMemReservations, rscType: "memory"},
	{setFun: setEphemeralStorageDefaults, getFun: getEphemeralStorageReservations, rscType: "ephemeral-storage"},
}

// SetKubeletExtraConfigDefaults adds Kubelet CPU, Mem, and Storage Reservation default values for a nodegroup
func SetKubeletExtraConfigDefaults(ng *NodeGroup, meta *ClusterMeta) error {
	for _, pSet := range rscParams {
		err := pSet.setFun(ng, pSet.rscType, meta, pSet.getFun)
		if err != nil {
			return err
		}
	}
	return nil
}

func setCpuReservationsDefaults(ng *NodeGroup, rscType string, meta *ClusterMeta, gfn getRscDefaultFunc) error {
	return setReservationDefault(ng, rscType, meta, gfn)
}

func setMemoryResevationDefaults(ng *NodeGroup, rscType string, meta *ClusterMeta, gfn getRscDefaultFunc) error {
	return setReservationDefault(ng, rscType, meta, gfn)
}

func setEphemeralStorageDefaults(ng *NodeGroup, rscType string, meta *ClusterMeta, gfn getRscDefaultFunc) error {
	return setReservationDefault(ng, rscType, meta, gfn)
}

func setReservationDefault(ng *NodeGroup, resType string, meta *ClusterMeta, fn getRscDefaultFunc) error {
	kec := (*ng).KubeletExtraConfig
	if kec == nil {
		kec = &InlineDocument{}
	}
	rsrcRes, err := fn((*ng).InstanceType, meta)
	if err != nil {
		return err
	}
	kubeReserved := getKubeReserved(*kec)
	// only set kubelet reservations for resource types that aren't already set in config
	if _, ok := kubeReserved[resType]; !ok {
		kubeReserved[resType] = rsrcRes
	}
	(*kec)["kubeReserved"] = kubeReserved
	ng.KubeletExtraConfig = kec
	return nil
}

func getKubeReserved(kec InlineDocument) map[string]interface{} {
	kubeReserved, ok := kec["kubeReserved"].(map[string]interface{})
	if !ok {
		kubeReserved = make(map[string]interface{})
	}
	return kubeReserved
}

type cpuEntry struct {
	cores int64
	res   string
}

// See: https://docs.microsoft.com/en-us/azure/aks/concepts-clusters-workloads
var cpuAllocations map[int64]string = map[int64]string{
	1:  "60m",
	2:  "100m",  //+40
	4:  "140m",  //+40
	8:  "180m",  //+40
	16: "260m",  //+80
	32: "420m",  //+160
	48: "580m",  //+160
	64: "740m",  //+320
	96: "1040m", //+320
}

func getCpuReservations(it string, meta *ClusterMeta) (string, error) {
	cores, err := getInstanceTypeCores(it, meta)
	if err != nil {
		return "", err
	}

	reservedCores := "0"
	ok := false
	if reservedCores, ok = cpuAllocations[cores]; !ok {
		err = fmt.Errorf("Could not find suggested core reservation for instance type: %s\n", it)
	}
	if err != nil {
		return "", err
	}
	return reservedCores, nil
}

func getInstanceTypeCores(it string, meta *ClusterMeta) (int64, error) {
	instTypeInfos, err := getInstanceTypeInfo(it, meta)
	if err != nil {
		return 0, err
	}
	vCpuInfo := (*instTypeInfos).VCpuInfo
	cpuCores := *vCpuInfo.DefaultVCpus
	return cpuCores, nil
}

type memEntry struct {
	max      float64
	fraction float64
}

// See: https://docs.microsoft.com/en-us/azure/aks/concepts-clusters-workloads
var memPercentages = []memEntry{
	{max: 4, fraction: 0.25},
	{max: 8, fraction: 0.20},
	{max: 16, fraction: 0.10},
	{max: 128, fraction: 0.06},
	{max: 65535, fraction: 0.02},
}

func getMemReservations(it string, meta *ClusterMeta) (string, error) {
	instMem, err := getInstanceTypeMem(it, meta)
	if err != nil {
		return "", err
	}
	var lower, reserved float64 = 0.0, 0.0
	for _, memEnt := range memPercentages {
		k, v := memEnt.max, memEnt.fraction
		if instMem <= k {
			reserved += v * (instMem - lower)
			break
		} else {
			reserved += v * (k - lower)
		}
		lower = k
	}
	reservedStr := formatMem(reserved)
	return reservedStr, nil
}

// formatFloat removes duplicate trailing zeros and ensures decimal format
func formatMem(f float64) string {
	ff := strconv.FormatFloat(f, 'f', -1, 32)
	if !strings.Contains(ff, ".") {
		ff += ".0"
	}
	return ff + "Mi"
}

func getInstanceTypeMem(it string, meta *ClusterMeta) (float64, error) {
	instTypeInfo, err := getInstanceTypeInfo(it, meta)
	if err != nil {
		return 0, err
	}
	memInfo := (*instTypeInfo).MemoryInfo
	memSize := float64(*memInfo.SizeInMiB)
	memStr := fmt.Sprintf("%.2f", float64(memSize/1024.0))
	return strconv.ParseFloat(memStr, 64)
}

func getEphemeralStorageReservations(it string, meta *ClusterMeta) (string, error) {
	storageSize, err := getInstanceTypeStorage(it, meta)
	if err != nil {
		return "", err
	}
	// at least 1GB but no larger than 15GB
	larger := math.Max(1.0, float64(storageSize)/16.0)
	smaller := math.Min(15.0, larger)
	storSize, storErr := formatStorageSize(smaller)
	return storSize, storErr
}

func formatStorageSize(f float64) (string, error) {
	// set precision to 2 decimal points
	fstr := fmt.Sprintf("%.2f", f)
	f64, err := strconv.ParseFloat(fstr, 64)
	if err != nil {
		return "", err
	}
	// remove any trailing zeros and convert to string
	return strconv.FormatFloat(f64, 'f', -1, 64) + "Gi", nil
}

func getInstanceTypeStorage(it string, meta *ClusterMeta) (int64, error) {
	defaultInstanceTypeStorage := int64(20) //GB
	instTypeInfo, err := getInstanceTypeInfo(it, meta)
	if err != nil {
		return 0, err
	}
	// If no default instance storage defined in instance type
	if !*instTypeInfo.InstanceStorageSupported {
		return defaultInstanceTypeStorage, nil
	}
	storageSize := (*instTypeInfo).InstanceStorageInfo.TotalSizeInGB
	return *storageSize, nil
}

func getInstanceTypeInfo(it string, meta *ClusterMeta) (*ec2.InstanceTypeInfo, error) {
	descInstTypeOutput, err := getInstanceTypeOutput(it, meta)
	if err != nil {
		return nil, err
	}
	instTypeInfos := descInstTypeOutput.InstanceTypes
	if len(instTypeInfos) == 0 {
		return nil, errors.New("No info found for instance type: " + it)
	}
	return instTypeInfos[0], nil
}

func getInstanceTypeOutput(it string, meta *ClusterMeta) (*ec2.DescribeInstanceTypesOutput, error) {
	sess, err := getSession(meta)
	if err != nil {
		return nil, err
	}
	svc := ec2.New(sess)
	descInstanceTypesInput := &ec2.DescribeInstanceTypesInput{}
	descInstanceTypesInput.SetInstanceTypes([]*string{&it})
	descInstanceTypesOutput, err := svc.DescribeInstanceTypes(descInstanceTypesInput)
	if err != nil {
		return nil, err
	}
	return descInstanceTypesOutput, nil
}

func getRegion(meta *ClusterMeta) string {
	region := ""
	region = (*meta).Region
	if region == "" {
		region = os.Getenv("REGION")
	}
	return region
}

func getSession(meta *ClusterMeta) (*session.Session, error) {
	region := getRegion(meta)
	var sess *session.Session = nil
	if region != "" {
		sess = session.Must(session.NewSession(
			&aws.Config{
				Region: aws.String(region),
			},
		))
	} else {
		sess = session.Must(session.NewSession())
	}
	if sess == nil {
		// returns session, error
		return session.NewSession()
	}
	return sess, nil
}

// DefaultClusterNAT will set the default value for Cluster NAT mode
func DefaultClusterNAT() *ClusterNAT {
	single := ClusterSingleNAT
	return &ClusterNAT{
		Gateway: &single,
	}
}

// ClusterEndpointAccessDefaults returns a ClusterEndpoints pointer with default values set.
func ClusterEndpointAccessDefaults() *ClusterEndpoints {
	return &ClusterEndpoints{
		PrivateAccess: Disabled(),
		PublicAccess:  Enabled(),
	}
}

// SetDefaultFargateProfile configures this ClusterConfig to have a single
// Fargate profile called "default", with two selectors matching respectively
// the "default" and "kube-system" Kubernetes namespaces.
func (c *ClusterConfig) SetDefaultFargateProfile() {
	c.FargateProfiles = []*FargateProfile{
		{
			Name: "fp-default",
			Selectors: []FargateProfileSelector{
				{Namespace: "default"},
				{Namespace: "kube-system"},
			},
		},
	}
}
