package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
)

// IPConfig is a struct of Ip configuration
type IPConfig struct {
	IP       string `json:"ip"`
	Port     int64  `json:"port"`
	Protocol string `json:"protocol"`
	Desc     string `json:"desc"`
}

// IPConfigs is a struct with a list of Ip configuration
type IPConfigs struct {
	Configs []*IPConfig `json:"ingress"`
}

// AwsConfig is the struct to parse the aws.json
type AwsConfig struct {
	Region string `json:"region"`
	SG     string `json:"sg"`
	Vpc    string `json:"vpc"`
}

func exitErrorf(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}

func parseIPFile(filename string) (*IPConfigs, error) {
	conf := new(IPConfigs)

	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %v", filename)
	}

	err = json.Unmarshal([]byte(content), conf)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal json: %v", filename)
	}

	return conf, nil
}

func parseConfig(filename string) (map[string]AwsConfig, error) {
	var conf = make(map[string]AwsConfig)

	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %v", filename)
	}

	err = json.Unmarshal([]byte(content), &conf)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal json: %v", filename)
	}

	return conf, nil
}

func addIngress(filename string, svc *ec2.EC2, id string) {
	ips, err := parseIPFile(filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse IP address error:", err)
		return
	}

	for _, ipconfig := range ips.Configs {
		_, err := svc.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
			GroupId: aws.String(id),
			IpPermissions: []*ec2.IpPermission{
				(&ec2.IpPermission{}).
					SetIpProtocol(ipconfig.Protocol).
					SetFromPort(ipconfig.Port).
					SetToPort(ipconfig.Port).
					SetIpRanges([]*ec2.IpRange{
						{
							CidrIp:      aws.String(ipconfig.IP),
							Description: aws.String(ipconfig.Desc),
						},
					}),
			},
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case "InvalidPermission.Duplicate":
					fmt.Fprintln(os.Stderr, "Ignore Duplicated Ingress Rule: ", ipconfig.IP)
				default:
					exitErrorf("Unable to set security group %q ingress, (%v) %v", ipconfig.Desc, aerr.Code(), err)
				}
			}
		} else {
			fmt.Fprintln(os.Stderr, "Added Ingress Rule: ", ipconfig.IP)
		}
	}

	fmt.Println("Successfully set security group ingress")
}

func main() {
	filePtr := flag.String("file", "ips.json", "json file of IP/port configuration")
	actionPtr := flag.String("action", "list", "actions: list, add")
	regionPtr := flag.String("region", "pdx", "AWS region airport code. ex, pdx, iad, dub, sfo")
	groupPtr := flag.String("sg", "", "list of security group ids")
	awsConfPtr := flag.String("conf", "awsconfig.json", "aws configuration in json format")

	flag.Parse()

	action := *actionPtr
	region := *regionPtr
	groupIds := strings.Split(*groupPtr, ",")
	IPFile := *filePtr

	Config, err := parseConfig(*awsConfPtr)
	if err != nil {
		exitErrorf("Unable to read AWS configuration")
	}

	if len(groupIds) == 0 {
		groupIds = []string{Config[region].SG}
	}
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(Config[region].Region)},
	)

	// Create an EC2 service client.
	svc := ec2.New(sess)

	result, err := svc.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: aws.StringSlice(groupIds),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case "InvalidGroupId.Malformed":
				fallthrough
			case "InvalidGroup.NotFound":
				exitErrorf("%s.", aerr.Message())
			}
		}
		exitErrorf("Unable to get descriptions for security groups, %v", err)
	}

	switch action {
	case "list":
		fmt.Println(result)
	case "add":
		for _, group := range result.SecurityGroups {
			addIngress(IPFile, svc, *group.GroupId)
		}
	}

}
