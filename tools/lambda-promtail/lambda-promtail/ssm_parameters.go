package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"
	"os"
	"strings"
)

const USERNAME_SSM_PARAMETER_NAME = "USERNAME_SSM_PARAMETER_NAME"
const PASSWORD_SSM_PARAMETER_NAME = "PASSWORD_SSM_PARAMETER_NAME"
const WRITE_ADDRESS_SSM_PARAMETER_NAME = "WRITE_ADDRESS_SSM_PARAMETER_NAME"
const EXTRA_LABELS_SSM_PARAMETER_NAME = "EXTRA_LABELS_SSM_PARAMETER_NAME"
const TENANT_ID_SSM_PARAMETER_NAME = "TENANT_ID_SSM_PARAMETER_NAME"

var ssmParametersVarNames = [5]string{
	USERNAME_SSM_PARAMETER_NAME,
	PASSWORD_SSM_PARAMETER_NAME,
	WRITE_ADDRESS_SSM_PARAMETER_NAME,
	EXTRA_LABELS_SSM_PARAMETER_NAME,
	TENANT_ID_SSM_PARAMETER_NAME,
}

type Config struct {
	Username     string
	Password     string
	WriteAddress string
	ExtraLabels  string
	TenantId     string
}

func GetConfigFromSsmParameters() Config {
	parameterNames := getEnvironmentVariables(ssmParametersVarNames[:])
	parameters := getParameters(parameterNames)

	return Config{
		Username:     parameters[USERNAME_SSM_PARAMETER_NAME],
		Password:     parameters[PASSWORD_SSM_PARAMETER_NAME],
		WriteAddress: parameters[WRITE_ADDRESS_SSM_PARAMETER_NAME],
		ExtraLabels:  parameters[EXTRA_LABELS_SSM_PARAMETER_NAME],
		TenantId:     parameters[TENANT_ID_SSM_PARAMETER_NAME],
	}
}

func getParameters(parameterNames map[string]string) map[string]string {
	names := getArrayOfParameterNames(parameterNames)

	sess := session.Must(session.NewSession())
	ssmClient := ssm.New(sess)

	decryption := true
	output, err := ssmClient.GetParameters(&ssm.GetParametersInput{
		Names:          names,
		WithDecryption: &decryption,
	})

	if err != nil {
		panic(err)
	}

	failOnInvalidParameters(output)
	return normalizeParametersNames(parameterNames, output)
}

func normalizeParametersNames(parameterNames map[string]string, output *ssm.GetParametersOutput) map[string]string {
	parameters := make(map[string]string)

	for key, paramName := range parameterNames {
		for _, param := range output.Parameters {
			if param.Name != &paramName {
				continue
			}

			parameters[key] = *param.Value
		}
	}

	return parameters
}

func failOnInvalidParameters(output *ssm.GetParametersOutput) {
	if len(output.InvalidParameters) == 0 {
		return
	}

	invalidParams := make([]string, len(output.InvalidParameters))

	for _, invalidParameter := range output.InvalidParameters {
		invalidParams = append(invalidParams, *invalidParameter)
	}

	panic(fmt.Sprintf("cannot get SSM parameters: %s", strings.Join(invalidParams, ", ")))
}

func getArrayOfParameterNames(parameterNames map[string]string) []*string {
	names := make([]*string, len(parameterNames))

	for _, name := range parameterNames {
		names = append(names, &name)
	}

	return names
}

func getEnvironmentVariables(varNames []string) (variables map[string]string) {
	variables = make(map[string]string, len(varNames))

	for _, varName := range varNames {
		value, present := os.LookupEnv(varName)

		if present {
			variables[varName] = value
			continue
		}
	}

	return
}
