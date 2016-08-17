package awsextra

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func tagIt(svc *ec2.EC2, ID *string, tagKey string, tagValue string) bool {
	tagItRetry(svc, ID, tagKey, tagValue, 0)
	return true
}

// Re-try incase of aws failure to recognize new resource ID.
func tagItRetry(svc *ec2.EC2, ID *string, tagKey string, tagValue string, retryCount int64) bool {
	retryCount++

	_, errtag := svc.CreateTags(&ec2.CreateTagsInput{
		Resources: []*string{ID},
		Tags: []*ec2.Tag{
			{
				Key:   aws.String(tagKey),
				Value: aws.String(tagValue),
			},
		},
	})
	if errtag != nil {
		if awsErr, ok := errtag.(awserr.Error); ok {
			fmt.Println("Retrying aws Code: " + awsErr.Code())
		} else {
			fmt.Println(errtag)
		}
		if retryCount > 20 {
			haltOnError(errtag, "Aborted: Maximum retries reached for tagging "+*ID)
		}
		time.Sleep(time.Second * 5)
		tagItRetry(svc, ID, tagKey, tagValue, retryCount)
	}
	return true
}
