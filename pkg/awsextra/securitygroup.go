package awsextra

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/spf13/viper"
)

func CreateSecurityGroup(svc *ec2.EC2, kindOf string, vpcID *string) (securityGroupID *string) {
	groupName := kindOf + "-" + viper.GetString("tagkey")
	params := &ec2.CreateSecurityGroupInput{
		Description: aws.String(groupName), // Required
		GroupName:   aws.String(groupName),
		VpcId:       vpcID,
	}
	resp, err := svc.CreateSecurityGroup(params)

	haltOnError(err, "Error creating security group")
	fmt.Println("Created security group " + *resp.GroupId)

	securityGroupID = resp.GroupId

	// Tag with the necessary tags
	tagIt(svc, securityGroupID, viper.GetString("tagkey"), viper.GetString("tagvalue"))
	// Tag an extra tag so we know what this security group is for.
	tagIt(svc, securityGroupID, "for", kindOf)

	return securityGroupID
}

func GetSecurityGroup(svc *ec2.EC2, kindOf string) *string {
	params := &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{ // Required
				Name: aws.String("tag:" + viper.GetString("tagkey")),
				Values: []*string{
					aws.String(viper.GetString("tagvalue")),
				},
			},
			{ // Required
				Name: aws.String("tag:for"),
				Values: []*string{
					aws.String(kindOf),
				},
			},
		},
	}
	resp, err := svc.DescribeSecurityGroups(params)

	haltOnError(err, "Error describing security groups")

	if len(resp.SecurityGroups) == 0 {
		return nil
	}

	return resp.SecurityGroups[0].GroupId
}

func AuthorizeSecurityGroupsInternalSSH(svc *ec2.EC2, groupID *string) {
	// Internal traffic from this group on all ports TCP
	params := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: groupID,
		IpPermissions: []*ec2.IpPermission{
			{ // Required
				FromPort:   aws.Int64(0),
				IpProtocol: aws.String("TCP"),
				ToPort:     aws.Int64(65535),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{
					{ // Required
						GroupId: groupID,
					},
				},
			},
		},
	}
	_, errInt := svc.AuthorizeSecurityGroupIngress(params)

	haltOnError(errInt, "Could not authorize security group for internal traffic on all TCP ports")

	// SSH on 22
	paramsSSH := &ec2.AuthorizeSecurityGroupIngressInput{
		CidrIp:     aws.String("0.0.0.0/0"),
		FromPort:   aws.Int64(22),
		GroupId:    groupID,
		IpProtocol: aws.String("TCP"),
		ToPort:     aws.Int64(22),
	}
	_, errSSH := svc.AuthorizeSecurityGroupIngress(paramsSSH)
	haltOnError(errSSH, "Could not authorize security group for SSH")

}

func DeleteSecurityGroup(svc *ec2.EC2, secGroupID *string) bool {
	return handleDeleteSecGroup(svc, secGroupID, 0)
}

func handleDeleteSecGroup(svc *ec2.EC2, secGroupID *string, retryCount int64) bool {
	params := &ec2.DeleteSecurityGroupInput{
		GroupId: secGroupID,
	}
	_, err := svc.DeleteSecurityGroup(params)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() == "DependencyViolation" {
				fmt.Print(".")
				retryCount++
				if retryCount > 60 {
					fmt.Println("retry limit reached for security group deletion.")
					return false
				}
				time.Sleep(time.Second * 5)
				handleDeleteSecGroup(svc, secGroupID, retryCount)
			}
		} else {
			fmt.Println(err.Error())
		}
		return false
	}
	fmt.Println("deleted security group " + *secGroupID)
	return true
}

func stripSecGroup(svc *ec2.EC2, secGroupID *string) {
	// First detangle the group from other groups.
	paramsDesc := &ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{secGroupID},
	}

	respDesc, errDesc := svc.DescribeSecurityGroups(paramsDesc)
	if errDesc != nil {
		fmt.Println("error describing security groups")
		fmt.Println(errDesc)
	}

	paramsDeleteRules := &ec2.RevokeSecurityGroupIngressInput{
		GroupId:       secGroupID,
		IpPermissions: respDesc.SecurityGroups[0].IpPermissions,
	}

	_, err := svc.RevokeSecurityGroupIngress(paramsDeleteRules)
	if err != nil {
		fmt.Println("error removing rules from security group")
		fmt.Println(err)
		return
	}
	fmt.Println("removed rules from: " + *secGroupID)

}
