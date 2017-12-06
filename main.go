package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/kataras/iris"
)

const (
	reqMethod      = "FULLBAN"
	varnishBanPort = ":6081"
)

var (
	region  *string
	asgName *string
)

func main() {
	// Input parameters
	port := *flag.String("port", "6051", "Port where varnish cache invalidater will listen to.")
	region = flag.String("region", "eu-central-1", "Region to search for varnish ec2 instances")
	asgName = flag.String("asgname", "", "Name of the autoscaling group where we look for varnish ec2 instances")
	flag.Parse()

	// Initialize IRIS
	api := iris.New()

	// Setup Routes
	api.Handle(reqMethod, "/", ClearCacheReq)

	// Listen and serve requests
	api.Run(iris.Addr(":" + port))
}

// ClearCacheReq serve
// Method: FULLBAN
// Desc: Function which gets all healthy nodes from
// an autoscaling group and sends a FULLBAN request to them
func ClearCacheReq(ctx iris.Context) {
	// Get and configure session and service
	sess := session.Must(session.NewSession())
	sess.Config.Region = region
	asSVC := autoscaling.New(sess)
	ec2SVC := ec2.New(sess)

	// Describe input
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			aws.String(*asgName),
		},
	}

	// Get all instances from this asg group
	result, err := asSVC.DescribeAutoScalingGroups(input)
	if err != nil {
		fmt.Printf("Error during describe autoscaling groups: %s\n", err.Error())
		return
	}

	// Iterate all instances which are healthy
	for _, instance := range result.AutoScalingGroups[0].Instances {
		if strings.ToLower(*instance.HealthStatus) != ec2.InstanceHealthStatusHealthy {
			fmt.Printf("Instance %s is unhealthy!\n", *instance.InstanceId)
			continue
		}

		// Generate params
		params := &ec2.DescribeInstancesInput{
			Filters: []*ec2.Filter{
				{
					Name: aws.String("instance-id"),
					Values: []*string{
						aws.String(*instance.InstanceId),
					},
				},
			},
		}

		// get instance with the given id
		resp, err := ec2SVC.DescribeInstances(params)
		if err != nil {
			fmt.Printf("Error during describe ec2 instance: %s\n", err.Error())
			ctx.StatusCode(http.StatusInternalServerError)
			return
		}

		// Send request to varnish instance
		err = sendClearCacheReq(resp)
		if err != nil {
			fmt.Printf("Error during send clear cache request to varnish instance %s: %s\n", *resp.Reservations[0].Instances[0].PrivateIpAddress, err.Error())
			ctx.StatusCode(http.StatusInternalServerError)
		}
	}
}

func sendClearCacheReq(result *ec2.DescribeInstancesOutput) error {
	// Send HTTP Request to instances
	client := &http.Client{}
	req, err := http.NewRequest(reqMethod, "http://"+*result.Reservations[0].Instances[0].PrivateIpAddress+varnishBanPort, nil)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	// Check if response was successful
	if resp.StatusCode != http.StatusOK {
		return errors.New("wrong status code replied from varnish instance")
	}

	return nil
}
