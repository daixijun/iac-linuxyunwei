package main

import (
	"fmt"

	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/cas"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/cdn"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/ecs"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/oss"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/resourcemanager"
	"github.com/pulumi/pulumi-alicloud/sdk/v3/go/alicloud/vpc"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

type SecurityGroupRulesConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"`
	Protocol    string `yaml:"protocol"`
	PortRange   string `yaml:"portRange"`
	CidrIP      string `yaml:"cidrIP"`
	Priority    int    `yaml:"priority"`
	Policy      string `yaml:"policy"`
}

type SecurityGroupConfig struct {
	Description       string                     `yaml:"description"`
	InnerAccessPolicy string                     `yaml:"innerAccessPolicy"`
	Name              string                     `yaml:"name"`
	Rules             []SecurityGroupRulesConfig `yaml:"rules"`
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		var sgc SecurityGroupConfig
		cfg.RequireObject("securityGroup", &sgc)

		rg, err := resourcemanager.NewResourceGroup(ctx, "default", &resourcemanager.ResourceGroupArgs{
			DisplayName:       pulumi.String("默认资源组"),
			ResourceGroupName: pulumi.String("default"),
		}, pulumi.Protect(true))
		if err != nil {
			return err
		}

		// Create OSS Bucket
		bucket, err := oss.NewBucket(ctx, "oss-bucket-linuxyunwei", &oss.BucketArgs{
			AccessMonitor: &oss.BucketAccessMonitorArgs{
				Status: pulumi.String("Disabled"),
			},
			Acl:            pulumi.String("private"),
			Bucket:         pulumi.String("linuxyunwei"),
			RedundancyType: pulumi.String("LRS"),
			StorageClass:   pulumi.String("Standard"),
		}, pulumi.Protect(true))
		if err != nil {
			return err
		}

		// Look up existing SSL certificate
		cert, err := cas.GetServiceCertificate(ctx, "cert-static.linuxyunwei.com", pulumi.ID("12309415"), &cas.ServiceCertificateState{
			CertificateName: pulumi.String("cert-static.linuxyunwei.com"),
		})
		if err != nil {
			return err
		}

		// Create CDN Domain
		cdnDomain, err := cdn.NewDomainNew(ctx, "static.linuxyunwei.com", &cdn.DomainNewArgs{
			CdnType: pulumi.String("web"),
			CertificateConfig: &cdn.DomainNewCertificateConfigArgs{
				CertId:            cert.ID(),
				CertName:          cert.CertificateName,
				CertRegion:        pulumi.String("cn-hangzhou"),
				CertType:          pulumi.String("cas"),
				ServerCertificate: cert.Cert,
			},
			DomainName:      pulumi.String("static.linuxyunwei.com"),
			ResourceGroupId: rg.ID(),
			Scope:           pulumi.String("domestic"),
			Sources: cdn.DomainNewSourceArray{
				&cdn.DomainNewSourceArgs{
					Content: pulumi.Sprintf("%s.%s", bucket.Bucket.Elem(), bucket.ExtranetEndpoint),
					Type:    pulumi.String("oss"),
				},
			},
		}, pulumi.Protect(true))
		if err != nil {
			return err
		}

		_, err = cdn.NewDomainConfig(ctx, "static.linuxyunwei.com-l2_oss_key", &cdn.DomainConfigArgs{
			DomainName: cdnDomain.DomainName,
			FunctionArgs: cdn.DomainConfigFunctionArgArray{
				&cdn.DomainConfigFunctionArgArgs{
					ArgName:  pulumi.String("private_oss_ram_unauthorized"),
					ArgValue: pulumi.String("off"),
				},
				&cdn.DomainConfigFunctionArgArgs{
					ArgName:  pulumi.String("private_oss_auth"),
					ArgValue: pulumi.String("on"),
				},
			},
			FunctionName: pulumi.String("l2_oss_key"),
		})
		if err != nil {
			return err
		}

		// Create VPC
		vpcNetwork, err := vpc.NewNetwork(ctx, "vpc-linuxyunwei", &vpc.NetworkArgs{
			CidrBlock:       pulumi.String("172.16.0.0/12"),
			ResourceGroupId: rg.ID(),
			VpcName:         pulumi.String("linuxyunwei"),
		}, pulumi.Protect(true))
		if err != nil {
			return err
		}

		// Create VSwitch in Zone I
		vsw, err := vpc.NewSwitch(ctx, "vsw-linuxyunwei-zone-i", &vpc.SwitchArgs{
			AvailabilityZone: pulumi.String("cn-hangzhou-i"),
			CidrBlock:        pulumi.String("172.16.0.0/24"),
			VpcId:            vpcNetwork.ID(),
			VswitchName:      pulumi.String("vsw-zone-i"),
		}, pulumi.Protect(true))
		if err != nil {
			return err
		}

		// Create Security Group
		sg, err := ecs.NewSecurityGroup(ctx, fmt.Sprintf("sg-%s", sgc.Name), &ecs.SecurityGroupArgs{
			Description:       pulumi.String(sgc.Description),
			InnerAccessPolicy: pulumi.String(sgc.InnerAccessPolicy),
			Name:              pulumi.String(sgc.Name),
			SecurityGroupType: pulumi.String("normal"),
			VpcId:             vpcNetwork.ID(),
			ResourceGroupId:   rg.ID(),
		}, pulumi.Protect(true))
		if err != nil {
			return err
		}

		// Create Security Group Rules
		for _, rule := range sgc.Rules {
			_, err = ecs.NewSecurityGroupRule(ctx, fmt.Sprintf("sg-rule-%s-%s-%s", sgc.Name, rule.Type, rule.Name), &ecs.SecurityGroupRuleArgs{
				Description:     pulumi.String(rule.Description),
				Policy:          pulumi.String(rule.Policy),
				SecurityGroupId: sg.ID(),
				IpProtocol:      pulumi.String(rule.Protocol),
				Type:            pulumi.String(rule.Type),
				PortRange:       pulumi.String(rule.PortRange),
				CidrIp:          pulumi.String(rule.CidrIP),
				Priority:        pulumi.Int(rule.Priority),
				// NicType:         pulumi.String("internet"),
			}, pulumi.Protect(true))
			if err != nil {
				return err
			}
		}
		// Create ECS Instance
		ecsInstance, err := ecs.NewInstance(ctx, "linuxyunwei.com", &ecs.InstanceArgs{
			AutoRenewPeriod:         pulumi.Int(1),
			AvailabilityZone:        vsw.ZoneId,
			HostName:                pulumi.String("iZbp1isnka8wpgq036d587Z"),
			ImageId:                 pulumi.String("aliyun_3_9_x64_20G_alibase_20231219.vhd"),
			InstanceChargeType:      pulumi.String("PrePaid"),
			InstanceName:            pulumi.String("linuxyunwei.com"),
			InstanceType:            pulumi.String("ecs.e-c1m1.large"),
			InternetChargeType:      pulumi.String("PayByBandwidth"),
			InternetMaxBandwidthOut: pulumi.Int(3),
			KeyName:                 pulumi.String("linuxyunwei"),
			MaintenanceAction:       pulumi.String("AutoRecover"),
			PeriodUnit:              pulumi.String("Month"),
			RenewalStatus:           pulumi.String("Normal"),
			SecurityGroups: pulumi.StringArray{
				sg.ID(),
			},
			SpotStrategy:       pulumi.String("NoSpot"),
			Status:             pulumi.String("Running"),
			StoppedMode:        pulumi.String("Not-applicable"),
			SystemDiskCategory: pulumi.String("cloud_essd_entry"),
			SystemDiskSize:     pulumi.Int(40),
			VswitchId:          vsw.ID(),
			ResourceGroupId:    rg.ID(),
		}, pulumi.Protect(true))
		if err != nil {
			return err
		}

		// Export the name of the services
		ctx.Export("bucketName", bucket.ID())
		ctx.Export("cdnDomainName", cdnDomain.ID().ApplyT(func(id string) string {
			return "https://" + id
		}).(pulumi.StringOutput))
		ctx.Export("ecsPrivateIp", ecsInstance.PrivateIp)
		ctx.Export("ecsPublicIp", ecsInstance.PublicIp)
		return nil
	})
}
