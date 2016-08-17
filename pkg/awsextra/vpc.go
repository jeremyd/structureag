package awsextra

import (
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/spf13/viper"
)

// Up

// Lookup vpc (just to ensure it exists)
func detectVPC(svc *ec2.EC2) (vpcID *string) {
	var ourTag = viper.GetString("tagvalue")
	params := &ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:" + viper.GetString("tagkey")),
				Values: []*string{
					aws.String(ourTag),
				},
			},
		},
	}
	resp, err := svc.DescribeVpcs(params)

	haltOnError(err, "Error describing VPCs")

	if len(resp.Vpcs) == 0 {
		return nil
	}

	return resp.Vpcs[0].VpcId
}

func vpcCheckConflict(svc *ec2.EC2) bool {
	params := &ec2.DescribeVpcsInput{}
	resp, err := svc.DescribeVpcs(params)
	haltOnError(err, "Error describing VPCs")
	for i := range resp.Vpcs {
		if *resp.Vpcs[i].CidrBlock == viper.GetString("vpc-cidr-block") {
			fmt.Println("Error.  Conflicting VPC CIDR block detected.  In use by " + *resp.Vpcs[i].VpcId)
			return true
		}
	}
	return false
}

// CreateVPCNetworking ... creates a VPC and all required sub-resources. Or returns existing.
func CreateVPCNetworking(svc *ec2.EC2) *string {

	// If the VPC exists return the existing VPC ID
	foundVpcID := detectVPC(svc)
	if foundVpcID != nil {
		fmt.Println("Found VPC: " + *foundVpcID)
		return foundVpcID
	}

	// If a VPC already exists with this same CIDR then halt.
	if vpcCheckConflict(svc) {
		usedConf := viper.ConfigFileUsed()
		haltError("Please modify " + usedConf + " config to select a different vpc-cidr-block block and re-run.")
	}

	// Create the VPC
	params := &ec2.CreateVpcInput{
		CidrBlock: aws.String(viper.GetString("vpc-cidr-block")), // Required
	}

	resp, err := svc.CreateVpc(params)

	haltOnError(err, "Failed to create VPC.")
	vpcID := resp.Vpc.VpcId
	fmt.Println("Created VPC: " + *vpcID)

	// Modify VPC for DnsSupport = true
	paramsModVPC := &ec2.ModifyVpcAttributeInput{
		VpcId: vpcID, // Required
		EnableDnsSupport: &ec2.AttributeBooleanValue{
			Value: aws.Bool(true),
		},
	}

	_, pModErr := svc.ModifyVpcAttribute(paramsModVPC)

	haltOnError(pModErr, "Error modifying VPC attributes for DnsSupport")

	// Modify VPC for DnsHostnames = true
	paramsModVPC2 := &ec2.ModifyVpcAttributeInput{
		VpcId: vpcID, // Required
		EnableDnsHostnames: &ec2.AttributeBooleanValue{
			Value: aws.Bool(true),
		},
	}

	_, pModErr2 := svc.ModifyVpcAttribute(paramsModVPC2)

	haltOnError(pModErr2, "Error Modifying VPC attribute for DnsHostnames")

	// Modify VPC for new dhcp options set
	dhcpOptionsSetID := createDhcpOptionsSet(svc)
	paramsModVPC3 := &ec2.AssociateDhcpOptionsInput{
		VpcId:         vpcID,            // Required
		DhcpOptionsId: dhcpOptionsSetID, // Required
	}

	_, pModErr3 := svc.AssociateDhcpOptions(paramsModVPC3)

	haltOnError(pModErr3, "error associating dhcp options set with VPC")

	// Create Route Table
	rtParams := &ec2.DescribeRouteTablesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("vpc-id"),
				Values: []*string{
					vpcID,
				},
			},
		},
	}
	rtResp, rtErr := svc.DescribeRouteTables(rtParams)

	haltOnError(rtErr, "Error creating Route Table")
	fmt.Println("Created route table: " + *rtResp.RouteTables[0].RouteTableId)

	// Tag the VPC and route tables
	tagIt(svc, vpcID, viper.GetString("tagkey"), viper.GetString("tagvalue"))
	tagIt(svc, rtResp.RouteTables[0].RouteTableId, viper.GetString("tagkey"), viper.GetString("tagvalue"))

	// Create subnets
	createSubnets(svc, vpcID)

	// Create IGW and attach to VPC
	IGWID := addInternetGatewayToVPC(svc, vpcID)

	// Add route entry to route table for IGW
	createRouteForIGW(svc, IGWID, rtResp.RouteTables[0].RouteTableId)

	return vpcID
}

func createDhcpOptionsSet(svc *ec2.EC2) *string {
	useHostNameSuffix := ""
	if viper.GetString("region") == "us-east-1" {
		useHostNameSuffix = "ec2.internal"
	} else {
		useHostNameSuffix = ".compute.internal"
	}
	params := &ec2.CreateDhcpOptionsInput{
		DhcpConfigurations: []*ec2.NewDhcpConfiguration{
			{ // Required
				Key: aws.String("domain-name-servers"),
				Values: []*string{
					aws.String("AmazonProvidedDNS"), // Required
				},
			},
			{ // Required
				Key: aws.String("domain-name"),
				Values: []*string{
					aws.String(viper.GetString("region") + useHostNameSuffix), // Required
				},
			},
		},
	}

	resp, err := svc.CreateDhcpOptions(params)

	haltOnError(err, "error creating DHCP Options Set")

	fmt.Println("Created dhcpOptionsSet" + *resp.DhcpOptions.DhcpOptionsId)

	tagIt(svc, resp.DhcpOptions.DhcpOptionsId, viper.GetString("tagkey"), viper.GetString("tagvalue"))

	return resp.DhcpOptions.DhcpOptionsId
}

func addInternetGatewayToVPC(svc *ec2.EC2, vpcID *string) *string {
	params := &ec2.CreateInternetGatewayInput{}
	resp, err := svc.CreateInternetGateway(params)

	haltOnError(err, "Error creating IGW")
	fmt.Println("Created IGW " + *resp.InternetGateway.InternetGatewayId)

	params2 := &ec2.AttachInternetGatewayInput{
		InternetGatewayId: resp.InternetGateway.InternetGatewayId, // Required
		VpcId:             vpcID,                                  // Required
	}
	_, err2 := svc.AttachInternetGateway(params2)

	haltOnError(err2, "Error attaching IGW to VPC")

	tagIt(svc, resp.InternetGateway.InternetGatewayId, viper.GetString("tagkey"), viper.GetString("tagvalue"))
	return resp.InternetGateway.InternetGatewayId
}

func createRouteForIGW(svc *ec2.EC2, IGWID *string, routeTableID *string) {
	params := &ec2.CreateRouteInput{
		DestinationCidrBlock: aws.String("0.0.0.0/0"), // Required
		RouteTableId:         routeTableID,            // Required
		GatewayId:            IGWID,
	}
	_, err := svc.CreateRoute(params)

	haltOnError(err, "Error creating route for IGW.")
	fmt.Println("Created route table entry for IGW")
}

func createSubnets(svc *ec2.EC2, vpcID *string) {
	// Get the availability zones list
	descAZParams := &ec2.DescribeAvailabilityZonesInput{}
	descAZResp, descAZErr := svc.DescribeAvailabilityZones(descAZParams)

	haltOnError(descAZErr, "Error describing AZs")

	// Create the subnets
	times, _ := strconv.ParseInt(viper.GetString("num-subnets"), 10, 0)
	var loop int64
	for loop = 0; loop < times; loop++ {
		//useAZIndex := loop % numAZs
		myCidrBlock := viper.GetString("subnet-" + fmt.Sprintf("%d", loop) + "-cidr")
		params := &ec2.CreateSubnetInput{
			CidrBlock:        aws.String(myCidrBlock),
			VpcId:            vpcID,
			AvailabilityZone: descAZResp.AvailabilityZones[loop].ZoneName,
		}
		resp, err := svc.CreateSubnet(params)

		haltOnError(err, "Error creating subnet.")
		fmt.Println("Created subnet " + *resp.Subnet.SubnetId)

		// Set auto-assign public IP on subnet
		params2 := &ec2.ModifySubnetAttributeInput{
			SubnetId: resp.Subnet.SubnetId,
			MapPublicIpOnLaunch: &ec2.AttributeBooleanValue{
				Value: aws.Bool(true),
			},
		}
		_, err2 := svc.ModifySubnetAttribute(params2)

		haltOnError(err2, "Error setting auto assign public IP failed for subnet.")

		tagIt(svc, resp.Subnet.SubnetId, viper.GetString("tagkey"), viper.GetString("tagvalue"))
	}
}

//
// Down
//

func deleteVPC(svc *ec2.EC2) {
	// Find the VPC associated with this kube cluster
	vpcID := detectVPC(svc)

	if vpcID == nil {
		fmt.Printf("VPC: not found")
		return
	}
	fmt.Print("delete VPC: " + *vpcID)
	deleteVPCRetry(svc, vpcID, 0)
	fmt.Println()
}

func deleteVPCRetry(svc *ec2.EC2, vpcID *string, retryCount int64) {
	params := &ec2.DeleteVpcInput{
		VpcId: vpcID,
	}
	_, err := svc.DeleteVpc(params)

	if awsErr, ok := err.(awserr.Error); ok {
		if awsErr.Code() == "DependencyViolation" {
			fmt.Print(".")
			retryCount++
			if retryCount > 60 {
				fmt.Println("retry limit reached for vpc deletion.")
				return
			}
			time.Sleep(time.Second * 5)
			deleteVPCRetry(svc, vpcID, retryCount)
		}
	}
}

func deleteIGW(svc *ec2.EC2) bool {
	params := &ec2.DescribeInternetGatewaysInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:" + viper.GetString("tagkey")),
				Values: []*string{
					aws.String(viper.GetString("tagvalue")),
				},
			},
		},
	}

	resp, err := svc.DescribeInternetGateways(params)
	if err != nil || len(resp.InternetGateways) == 0 {
		fmt.Println("IGW: not found")
		if err != nil {
			fmt.Println(err)
		}
		return false
	}

	fmt.Print("delete IGW: " + *resp.InternetGateways[0].InternetGatewayId)

	paramsDetach := &ec2.DetachInternetGatewayInput{
		InternetGatewayId: resp.InternetGateways[0].InternetGatewayId,
		VpcId:             resp.InternetGateways[0].Attachments[0].VpcId,
	}

	_, errDetach := svc.DetachInternetGateway(paramsDetach)

	if errDetach != nil {
		fmt.Println("error detaching IGW from vpc")
		fmt.Println(errDetach)
		return false
	}

	deleteIGWRetry(svc, resp.InternetGateways[0].InternetGatewayId, 0)
	fmt.Println()
	return true
}

func deleteIGWRetry(svc *ec2.EC2, IGWID *string, retryCount int64) bool {
	paramsDelete := &ec2.DeleteInternetGatewayInput{
		InternetGatewayId: IGWID,
	}

	_, errDelete := svc.DeleteInternetGateway(paramsDelete)

	if errDelete != nil {
		if awsErr, ok := errDelete.(awserr.Error); ok {
			if awsErr.Code() == "DependencyViolation" {
				fmt.Print(".")
				retryCount++
				if retryCount > 60 {
					fmt.Println("retry limit reached for IGW deletion.")
					return false
				}
				time.Sleep(time.Second * 5)
				deleteIGWRetry(svc, IGWID, retryCount)
			}
		} else {
			fmt.Println(errDelete.Error())
		}
	}
	return true
}

func deleteRouteTable(svc *ec2.EC2) bool {
	params := &ec2.DescribeRouteTablesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:" + viper.GetString("tagkey")),
				Values: []*string{
					aws.String(viper.GetString("tagvalue")),
				},
			},
		},
	}

	resp, err := svc.DescribeRouteTables(params)

	if err != nil || len(resp.RouteTables) == 0 {
		fmt.Println("Route Table: not found")
		if err != nil {
			fmt.Println(err)
		}
		return false
	}
	fmt.Print("delete Route Table: " + *resp.RouteTables[0].RouteTableId)
	deleteRouteTableRetry(svc, resp.RouteTables[0].RouteTableId, 0)
	fmt.Println()
	return true
}

func deleteRouteTableRetry(svc *ec2.EC2, routeTableID *string, retryCount int64) {
	paramsDelete := &ec2.DeleteRouteTableInput{
		RouteTableId: routeTableID,
	}

	_, errDelete := svc.DeleteRouteTable(paramsDelete)

	if errDelete != nil {
		if awsErr, ok := errDelete.(awserr.Error); ok {
			if awsErr.Code() == "DependencyViolation" {
				fmt.Print(".")
				retryCount++
				if retryCount > 60 {
					fmt.Println("retry limit reached for dhcpOptions deletion.")
					return
				}
				time.Sleep(time.Second * 5)
				deleteRouteTableRetry(svc, routeTableID, retryCount)
			}
		} else {
			fmt.Println(errDelete)
		}
	}
}

func deleteDhcpOptionSet(svc *ec2.EC2) bool {
	params := &ec2.DescribeDhcpOptionsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:" + viper.GetString("tagkey")),
				Values: []*string{
					aws.String(viper.GetString("tagvalue")),
				},
			},
		},
	}

	resp, err := svc.DescribeDhcpOptions(params)

	if err != nil || len(resp.DhcpOptions) == 0 {
		fmt.Println("error describing dhcp option sets")
		fmt.Println(err)
		return false
	}
	fmt.Print("delete DHCP options set: " + *resp.DhcpOptions[0].DhcpOptionsId)
	deleteDhcpOptionsRetry(svc, resp.DhcpOptions[0].DhcpOptionsId, 0)
	fmt.Println()
	return true
}

func deleteDhcpOptionsRetry(svc *ec2.EC2, dhcpOptionsID *string, retryCount int64) {
	paramsDelete := &ec2.DeleteDhcpOptionsInput{
		DhcpOptionsId: dhcpOptionsID,
	}

	_, respErr := svc.DeleteDhcpOptions(paramsDelete)

	if respErr != nil {
		if awsErr, ok := respErr.(awserr.Error); ok {
			if awsErr.Code() == "DependencyViolation" {
				fmt.Print(".")
				retryCount++
				if retryCount > 60 {
					fmt.Println("retry limit reached for dhcpOptions deletion.")
					return
				}
				time.Sleep(time.Second * 5)
				deleteDhcpOptionsRetry(svc, dhcpOptionsID, retryCount)
			}
		} else {
			fmt.Println(respErr.Error())
		}
	}
}

func deleteSubnet(svc *ec2.EC2, subnetID *string, retryCount int64) bool {
	params := &ec2.DeleteSubnetInput{
		SubnetId: subnetID,
	}

	_, err := svc.DeleteSubnet(params)

	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == "DependencyViolation" {
				fmt.Print(".")
				retryCount++
				if retryCount > 60 {
					fmt.Println("retry limit reached for subnet deletion.")
					return false
				}
				time.Sleep(time.Second * 5)
				deleteSubnet(svc, subnetID, retryCount)
			}
		} else {
			fmt.Println(err.Error())
		}
	}

	return true
}

func deleteSubnets(svc *ec2.EC2) bool {
	params := &ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:" + viper.GetString("tagkey")),
				Values: []*string{
					aws.String(viper.GetString("tagvalue")),
				},
			},
		},
	}

	resp, err := svc.DescribeSubnets(params)

	haltOnError(err, "Error describing subnets")

	allSuccess := true
	for i := 0; i < len(resp.Subnets); i++ {
		fmt.Println("delete subnet: " + *resp.Subnets[i].SubnetId)
		if deleteSubnet(svc, resp.Subnets[i].SubnetId, 0) == false {
			allSuccess = false
		}
	}
	fmt.Println()
	return allSuccess
}

// DeleteVPCNetworking ... Deletes all VPC components.
func DeleteVPCNetworking(svc *ec2.EC2) bool {
	deleteIGW(svc)
	deleteSubnets(svc)
	deleteVPC(svc)
	deleteDhcpOptionSet(svc)
	return true
}
