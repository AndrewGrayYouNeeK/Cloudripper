import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";
import * as gcp from "@pulumi/gcp";

const config = new pulumi.Config();
const awsRegion = config.get("awsRegion") || "us-east-1";
const gcpProject = config.require("gcpProject");
const gcpRegion = config.get("gcpRegion") || "us-central1";
const clusterName = config.get("clusterName") || "cloudripper";

const awsProvider = new aws.Provider("aws", { region: awsRegion });

const vpc = new aws.ec2.Vpc("cloudripper-vpc", {
    cidrBlock: "10.0.0.0/16",
    enableDnsHostnames: true,
    enableDnsSupport: true,
    tags: { Name: `${clusterName}-vpc`, Project: "cloudripper" },
}, { provider: awsProvider });

const gcpNetwork = new gcp.compute.Network("cloudripper-network", {
    name: `${clusterName}-vpc`,
    autoCreateSubnetworks: false,
    project: gcpProject,
});

const gcpSubnet = new gcp.compute.Subnetwork("cloudripper-subnet", {
    name: `${clusterName}-subnet`,
    ipCidrRange: "10.1.0.0/24",
    region: gcpRegion,
    network: gcpNetwork.id,
    project: gcpProject,
});

export const awsVpcId = vpc.id;
export const gcpNetworkId = gcpNetwork.id;
export const gcpSubnetId = gcpSubnet.id;
