package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
)

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

func parseIPFile(filename string) (map[string]string, error) {
	var ips = make(map[string]string)

	file, err := os.Open(filename)
	if err != nil {
		return ips, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		items := strings.Split(line, ",")
		ips[items[0]] = items[1]
	}

	if err := scanner.Err(); err != nil {
		return ips, err
	}

	return ips, nil
}

func addIngress(filename string, svc *ec2.EC2, id string, port int64) {
	ips, err := parseIPFile(filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse IP address error:", err)
		return
	}

	for ip, name := range ips {
		_, err := svc.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
			GroupId: aws.String(id),
			IpPermissions: []*ec2.IpPermission{
				(&ec2.IpPermission{}).
					SetIpProtocol("tcp").
					SetFromPort(port).
					SetToPort(port).
					SetIpRanges([]*ec2.IpRange{
						{
							CidrIp:      aws.String(ip),
							Description: aws.String(name),
						},
					}),
			},
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case "InvalidPermission.Duplicate":
					fmt.Fprintln(os.Stderr, "Ignore Duplicated Ingress Rule: ", ip)
				default:
					exitErrorf("Unable to set security group %q ingress, (%v) %v", name, aerr.Code(), err)
				}
			}
		} else {
			fmt.Fprintln(os.Stderr, "Added Ingress Rule: ", ip, port)
		}
	}

	fmt.Println("Successfully set security group ingress")
}

func main() {
	portPtr := flag.String("ports", "22", "list of open port")
	filePtr := flag.String("file", "ips.txt", "filename of IP addresses")
	actionPtr := flag.String("action", "list", "actions: list, add")
	regionPtr := flag.String("region", "pdx", "AWS region airport code. ex, pdx, iad, dub, sfo")
	groupPtr := flag.String("sg", "", "list of security group ids")
	awsConfPtr := flag.String("conf", "awsconfig.json", "aws configuration in json format")

	flag.Parse()

	action := *actionPtr
	region := *regionPtr
	groupIds := strings.Split(*groupPtr, ",")
	openPorts := strings.Split(*portPtr, ",")
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
			for _, port := range openPorts {
				np, err := strconv.ParseInt(port, 10, 64)
				if err != nil {
					fmt.Errorf("parsing port error: %v", err)
				} else {
					addIngress(IPFile, svc, *group.GroupId, np)
				}
			}
		}
	}

}
