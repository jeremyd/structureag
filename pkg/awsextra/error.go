package awsextra

import (
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws/awserr"
)

// Handle various AWS errors
func errorCode(err error) (errorCode *string) {
	if awsErr, ok := err.(awserr.Error); ok {
		// Eg, "DependencyViolation"
		newCode := awsErr.Code()
		return &newCode
	}
	return nil
}

// If an error happened, halt and print this message.
func haltOnError(err error, message string) {
	if err != nil {
		fmt.Println(err)
		haltError(message)
	}
}

// Print message to stderr and exit.
func haltError(message string) {
	os.Stderr.WriteString(message)
	os.Exit(1)
}
