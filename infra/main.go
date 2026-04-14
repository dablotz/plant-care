package main

import (
	"encoding/json"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg            := config.New(ctx, "")
		region         := cfg.Require("aws:region")
		anthropicKey   := cfg.RequireSecret("plantcare:anthropicApiKey")
		frontendOrigin := cfg.Get("plantcare:frontendOrigin") // optional; restrict S3 CORS when domain is known

		// ── DynamoDB Table ───────────────────────────────────────────────────
		table, err := dynamodb.NewTable(ctx, "plantcare-plants", &dynamodb.TableArgs{
			Name:        pulumi.String("plantcare-plants"),
			BillingMode: pulumi.String("PAY_PER_REQUEST"),
			HashKey:     pulumi.String("id"),
			Attributes: dynamodb.TableAttributeArray{
				&dynamodb.TableAttributeArgs{
					Name: pulumi.String("id"),
					Type: pulumi.String("S"),
				},
			},
		})
		if err != nil {
			return err
		}

		// ── S3 Upload Bucket ─────────────────────────────────────────────────
		uploadBucket, err := s3.NewBucketV2(ctx, "plantcare-uploads", &s3.BucketV2Args{
			ForceDestroy: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}
		// Block all public access — objects are only reachable via pre-signed URLs
		_, err = s3.NewBucketPublicAccessBlock(ctx, "plantcare-uploads-block", &s3.BucketPublicAccessBlockArgs{
			Bucket:                uploadBucket.Bucket,
			BlockPublicAcls:       pulumi.Bool(true),
			BlockPublicPolicy:     pulumi.Bool(true),
			IgnorePublicAcls:      pulumi.Bool(true),
			RestrictPublicBuckets: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}
		// CORS: allow browsers to PUT directly via pre-signed URL.
		// Restrict AllowedOrigins to the frontend domain when plantcare:frontendOrigin is set.
		corsOrigins := pulumi.StringArray{pulumi.String("*")}
		if frontendOrigin != "" {
			corsOrigins = pulumi.StringArray{pulumi.String(frontendOrigin)}
		} else {
			ctx.Log.Warn("plantcare:frontendOrigin not set — S3 CORS allows all origins; set it to your frontend domain to restrict", nil)
		}
		_, err = s3.NewBucketCorsConfigurationV2(ctx, "plantcare-uploads-cors", &s3.BucketCorsConfigurationV2Args{
			Bucket: uploadBucket.Bucket,
			CorsRules: s3.BucketCorsConfigurationV2CorsRuleArray{
				&s3.BucketCorsConfigurationV2CorsRuleArgs{
					AllowedHeaders: pulumi.StringArray{pulumi.String("*")},
					AllowedMethods: pulumi.StringArray{pulumi.String("PUT")},
					AllowedOrigins: corsOrigins,
					MaxAgeSeconds:  pulumi.Int(3000),
				},
			},
		})
		if err != nil {
			return err
		}
		// Lifecycle: auto-delete temp uploads after 1 day
		_, err = s3.NewBucketLifecycleConfigurationV2(ctx, "plantcare-uploads-lifecycle", &s3.BucketLifecycleConfigurationV2Args{
			Bucket: uploadBucket.Bucket,
			Rules: s3.BucketLifecycleConfigurationV2RuleArray{
				&s3.BucketLifecycleConfigurationV2RuleArgs{
					Id:     pulumi.String("expire-uploads"),
					Status: pulumi.String("Enabled"),
					Filter: &s3.BucketLifecycleConfigurationV2RuleFilterArgs{
						Prefix: pulumi.String("uploads/"),
					},
					Expiration: &s3.BucketLifecycleConfigurationV2RuleExpirationArgs{
						Days: pulumi.Int(1), // minimum S3 allows; uploads are consumed within seconds
					},
				},
			},
		})
		if err != nil {
			return err
		}

		// ── IAM: Lambda Execution Role ───────────────────────────────────────
		lambdaRoleDoc := `{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"Service": "lambda.amazonaws.com"},
    "Action": "sts:AssumeRole"
  }]
}`
		lambdaRole, err := iam.NewRole(ctx, "plantcare-lambda-role", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(lambdaRoleDoc),
		})
		if err != nil {
			return err
		}
		// Basic execution: write logs to CloudWatch
		_, err = iam.NewRolePolicyAttachment(ctx, "plantcare-lambda-basic", &iam.RolePolicyAttachmentArgs{
			Role:      lambdaRole.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
		})
		if err != nil {
			return err
		}
		// App permissions: DynamoDB + S3
		appPolicy := pulumi.All(table.Arn, uploadBucket.Arn).ApplyT(func(args []interface{}) (string, error) {
			tableArn  := args[0].(string)
			bucketArn := args[1].(string)
			doc := map[string]interface{}{
				"Version": "2012-10-17",
				"Statement": []map[string]interface{}{
					{
						"Effect": "Allow",
						"Action": []string{
							"dynamodb:PutItem",
							"dynamodb:GetItem",
							"dynamodb:DeleteItem",
							"dynamodb:Scan",
						},
						"Resource": tableArn,
					},
					{
						"Effect": "Allow",
						"Action": []string{
							"s3:PutObject",
							"s3:GetObject",
						},
						"Resource": bucketArn + "/uploads/*",
					},
				},
			}
			b, err := json.Marshal(doc)
			return string(b), err
		}).(pulumi.StringOutput)
		_, err = iam.NewRolePolicy(ctx, "plantcare-lambda-policy", &iam.RolePolicyArgs{
			Role:   lambdaRole.Name,
			Policy: appPolicy,
		})
		if err != nil {
			return err
		}

		// ── CloudWatch Log Group ─────────────────────────────────────────────
		_, err = cloudwatch.NewLogGroup(ctx, "plantcare-logs", &cloudwatch.LogGroupArgs{
			Name:            pulumi.String("/aws/lambda/plantcare"),
			RetentionInDays: pulumi.Int(7),
		})
		if err != nil {
			return err
		}

		// ── Lambda Function ──────────────────────────────────────────────────
		// Run `make lambda-build` before `pulumi up` to create plantcare-lambda.zip
		fn, err := lambda.NewFunction(ctx, "plantcare", &lambda.FunctionArgs{
			Name:    pulumi.String("plantcare"),
			Runtime: pulumi.String("provided.al2023"),
			Handler: pulumi.String("bootstrap"),
			Role:    lambdaRole.Arn,
			Code:    pulumi.NewFileArchive("../plantcare-lambda.zip"),
			Timeout: pulumi.Int(120), // accommodate Anthropic API latency
			MemorySize: pulumi.Int(256),
			Environment: &lambda.FunctionEnvironmentArgs{
				Variables: pulumi.StringMap{
					"ANTHROPIC_API_KEY": anthropicKey, // pragma: allowlist secret
					"STORAGE_TYPE":      pulumi.String("dynamodb"),
					"DYNAMODB_TABLE":    pulumi.String("plantcare-plants"),
					"UPLOAD_BUCKET":     uploadBucket.Bucket,
					"AWS_REGION":        pulumi.String(region),
					"WEB_DIR":           pulumi.String("/var/task/web"),
				},
			},
		})
		if err != nil {
			return err
		}

		// ── Lambda Function URL ──────────────────────────────────────────────
		fnUrl, err := lambda.NewFunctionUrl(ctx, "plantcare-url", &lambda.FunctionUrlArgs{
			FunctionName:      fn.Name,
			AuthorizationType: pulumi.String("NONE"),
			Cors: &lambda.FunctionUrlCorsArgs{
				AllowOrigins: corsOrigins,
				AllowMethods: pulumi.StringArray{pulumi.String("*")},
				AllowHeaders: pulumi.StringArray{pulumi.String("*")},
				MaxAge:       pulumi.Int(3000),
			},
		})
		if err != nil {
			return err
		}

		// Allow public invocation via the Function URL
		_, err = lambda.NewPermission(ctx, "plantcare-url-invoke", &lambda.PermissionArgs{
			Action:              pulumi.String("lambda:InvokeFunctionUrl"),
			Function:            fn.Name,
			Principal:           pulumi.String("*"),
			FunctionUrlAuthType: pulumi.String("NONE"),
		})
		if err != nil {
			return err
		}

		// ── Stack Outputs ─────────────────────────────────────────────────────
		ctx.Export("functionUrl",  fnUrl.FunctionUrl)
		ctx.Export("uploadBucket", uploadBucket.Bucket)
		return nil
	})
}
