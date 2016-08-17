package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/jeremyd/structureag/pkg/awsextra"
	"github.com/spf13/viper"
)

func main() {
	// Command line flags (non-VIPER)
	var action = flag.String("action", "", "Action can be: init, launch-minion")
	flag.Parse()
	switch *action {
	case "up":
	case "down":
	case "delete":
	default:
		fmt.Println("Usage:  structureag -action=<ACTION>  Please specify an action: up, down, delete.")
		os.Exit(1)
	}

	// Viper set to read in config.* (toml, json, yaml)
	viper.SetConfigFile("./config.toml")
	// Viper set to read in Environment vars prefixed with STRUCTURE_
	viper.SetEnvPrefix("STRUCTURE")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}

	svc := ec2.New(session.New(), &aws.Config{Region: aws.String(viper.GetString("region"))})
	//elbSvc := elb.New(session.New(), &aws.Config{Region: aws.String(viper.GetString("region"))})

	if *action == "up" {

		// Create VPC
		vpcID := awsextra.CreateVPCNetworking(svc)

		// Create SSH key
		//awsextra.createSSHKey(svc)

		// Create Security Groups
		securityGroupID := awsextra.CreateSecurityGroup(svc, "default", vpcID)
		awsextra.AuthorizeSecurityGroupsInternalSSH(svc, securityGroupID)

	}

	if *action == "down" {
		securityGroupID := awsextra.GetSecurityGroup(svc, "default")
		awsextra.DeleteSecurityGroup(svc, securityGroupID)

		// Delete VPC and all sub resources
		awsextra.DeleteVPCNetworking(svc)
	}
}
