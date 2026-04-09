package main

import (
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ecr"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ecs"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lb"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg          := config.New(ctx, "")
		region       := cfg.Require("aws:region")
		imageTag     := cfg.Require("plantcare:imageTag")
		anthropicKey := cfg.RequireSecret("plantcare:anthropicApiKey")

		// ── ECR Repository ───────────────────────────────────────────────────
		repo, err := ecr.NewRepository(ctx, "plantcare-repo", &ecr.RepositoryArgs{
			Name:        pulumi.String("plantcare"),
			ForceDelete: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}

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

		// ── IAM: Task Execution Role (ECS agent — pulls image, writes logs) ──
		execRoleDoc := `{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"Service": "ecs-tasks.amazonaws.com"},
    "Action": "sts:AssumeRole"
  }]
}`
		execRole, err := iam.NewRole(ctx, "plantcare-exec-role", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(execRoleDoc),
		})
		if err != nil {
			return err
		}
		_, err = iam.NewRolePolicyAttachment(ctx, "plantcare-exec-policy", &iam.RolePolicyAttachmentArgs{
			Role:      execRole.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"),
		})
		if err != nil {
			return err
		}

		// ── IAM: Task Role (app — calls DynamoDB) ───────────────────────────
		taskRoleDoc := `{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"Service": "ecs-tasks.amazonaws.com"},
    "Action": "sts:AssumeRole"
  }]
}`
		taskRole, err := iam.NewRole(ctx, "plantcare-task-role", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(taskRoleDoc),
		})
		if err != nil {
			return err
		}
		taskPolicy := table.Arn.ApplyT(func(tableArn string) (string, error) {
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
				},
			}
			b, err := json.Marshal(doc)
			return string(b), err
		}).(pulumi.StringOutput)
		_, err = iam.NewRolePolicy(ctx, "plantcare-task-policy", &iam.RolePolicyArgs{
			Role:   taskRole.Name,
			Policy: taskPolicy,
		})
		if err != nil {
			return err
		}

		// ── Default VPC + Subnets ────────────────────────────────────────────
		vpc, err := ec2.LookupVpc(ctx, &ec2.LookupVpcArgs{Default: pulumi.BoolRef(true)})
		if err != nil {
			return err
		}
		subnetsResult, err := ec2.GetSubnetIds(ctx, &ec2.GetSubnetIdsArgs{VpcId: vpc.Id})
		if err != nil {
			return err
		}
		subnetIds := make(pulumi.StringArray, len(subnetsResult.Ids))
		for i, id := range subnetsResult.Ids {
			subnetIds[i] = pulumi.String(id)
		}

		// ── Security Group: ALB ──────────────────────────────────────────────
		albSg, err := ec2.NewSecurityGroup(ctx, "plantcare-alb-sg", &ec2.SecurityGroupArgs{
			VpcId:       pulumi.String(vpc.Id),
			Description: pulumi.String("PlantCare ALB"),
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					Protocol:   pulumi.String("tcp"),
					FromPort:   pulumi.Int(80),
					ToPort:     pulumi.Int(80),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol:   pulumi.String("tcp"),
					FromPort:   pulumi.Int(443),
					ToPort:     pulumi.Int(443),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
			},
			Egress: ec2.SecurityGroupEgressArray{
				&ec2.SecurityGroupEgressArgs{
					Protocol:   pulumi.String("-1"),
					FromPort:   pulumi.Int(0),
					ToPort:     pulumi.Int(0),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
			},
		})
		if err != nil {
			return err
		}

		// ── Security Group: ECS Task ─────────────────────────────────────────
		ecsSg, err := ec2.NewSecurityGroup(ctx, "plantcare-ecs-sg", &ec2.SecurityGroupArgs{
			VpcId:       pulumi.String(vpc.Id),
			Description: pulumi.String("PlantCare ECS task"),
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					Protocol:       pulumi.String("tcp"),
					FromPort:       pulumi.Int(8080),
					ToPort:         pulumi.Int(8080),
					SecurityGroups: pulumi.StringArray{albSg.ID()},
				},
			},
			Egress: ec2.SecurityGroupEgressArray{
				&ec2.SecurityGroupEgressArgs{
					Protocol:   pulumi.String("-1"),
					FromPort:   pulumi.Int(0),
					ToPort:     pulumi.Int(0),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
			},
		})
		if err != nil {
			return err
		}

		// ── ALB ──────────────────────────────────────────────────────────────
		alb, err := lb.NewLoadBalancer(ctx, "plantcare-alb", &lb.LoadBalancerArgs{
			Internal:         pulumi.Bool(false),
			LoadBalancerType: pulumi.String("application"),
			SecurityGroups:   pulumi.StringArray{albSg.ID()},
			Subnets:          subnetIds,
		})
		if err != nil {
			return err
		}

		// ── Target Group ─────────────────────────────────────────────────────
		tg, err := lb.NewTargetGroup(ctx, "plantcare-tg", &lb.TargetGroupArgs{
			Port:       pulumi.Int(8080),
			Protocol:   pulumi.String("HTTP"),
			TargetType: pulumi.String("ip"), // required for Fargate
			VpcId:      pulumi.String(vpc.Id),
			HealthCheck: &lb.TargetGroupHealthCheckArgs{
				Path:               pulumi.String("/api/health"),
				HealthyThreshold:   pulumi.Int(2),
				UnhealthyThreshold: pulumi.Int(3),
				Interval:           pulumi.Int(30),
			},
		})
		if err != nil {
			return err
		}

		// ── HTTP Listener ────────────────────────────────────────────────────
		_, err = lb.NewListener(ctx, "plantcare-listener", &lb.ListenerArgs{
			LoadBalancerArn: alb.Arn,
			Port:            pulumi.Int(80),
			DefaultActions: lb.ListenerDefaultActionArray{
				&lb.ListenerDefaultActionArgs{
					Type:           pulumi.String("forward"),
					TargetGroupArn: tg.Arn,
				},
			},
		})
		if err != nil {
			return err
		}

		// ── CloudWatch Log Group ─────────────────────────────────────────────
		logGroup, err := cloudwatch.NewLogGroup(ctx, "plantcare-logs", &cloudwatch.LogGroupArgs{
			Name:            pulumi.String("/ecs/plantcare"),
			RetentionInDays: pulumi.Int(7),
		})
		if err != nil {
			return err
		}

		// ── ECS Cluster ──────────────────────────────────────────────────────
		cluster, err := ecs.NewCluster(ctx, "plantcare-cluster", &ecs.ClusterArgs{})
		if err != nil {
			return err
		}

		// ── ECS Task Definition ──────────────────────────────────────────────
		containerDefs := pulumi.All(repo.RepositoryUrl, logGroup.Name, anthropicKey).ApplyT(
			func(args []interface{}) (string, error) {
				repoURL      := args[0].(string)
				lgName       := args[1].(string)
				anthropicKey := args[2].(string)
				defs := []map[string]interface{}{{
					"name":  "plantcare",
					"image": fmt.Sprintf("%s:%s", repoURL, imageTag),
					"portMappings": []map[string]interface{}{
						{"containerPort": 8080, "protocol": "tcp"},
					},
					"environment": []map[string]string{
						{"name": "ANTHROPIC_API_KEY", "value": anthropicKey},
						{"name": "STORAGE_TYPE",      "value": "dynamodb"},
						{"name": "DYNAMODB_TABLE",    "value": "plantcare-plants"},
						{"name": "AWS_REGION",        "value": region},
						{"name": "PORT",              "value": "8080"},
						{"name": "WEB_DIR",           "value": "/app/web"},
					},
					"logConfiguration": map[string]interface{}{
						"logDriver": "awslogs",
						"options": map[string]string{
							"awslogs-group":         lgName,
							"awslogs-region":        region,
							"awslogs-stream-prefix": "ecs",
						},
					},
				}}
				b, err := json.Marshal(defs)
				return string(b), err
			},
		).(pulumi.StringOutput)

		taskDef, err := ecs.NewTaskDefinition(ctx, "plantcare-task", &ecs.TaskDefinitionArgs{
			Family:                  pulumi.String("plantcare"),
			Cpu:                     pulumi.String("512"),
			Memory:                  pulumi.String("1024"),
			NetworkMode:             pulumi.String("awsvpc"),
			RequiresCompatibilities: pulumi.StringArray{pulumi.String("FARGATE")},
			ExecutionRoleArn:        execRole.Arn,
			TaskRoleArn:             taskRole.Arn,
			ContainerDefinitions:    containerDefs,
		})
		if err != nil {
			return err
		}

		// ── ECS Service ──────────────────────────────────────────────────────
		_, err = ecs.NewService(ctx, "plantcare-service", &ecs.ServiceArgs{
			Cluster:        cluster.Arn,
			DesiredCount:   pulumi.Int(1),
			LaunchType:     pulumi.String("FARGATE"),
			TaskDefinition: taskDef.Arn,
			NetworkConfiguration: &ecs.ServiceNetworkConfigurationArgs{
				AssignPublicIp: pulumi.Bool(true),
				Subnets:        subnetIds,
				SecurityGroups: pulumi.StringArray{ecsSg.ID()},
			},
			LoadBalancers: ecs.ServiceLoadBalancerArray{
				&ecs.ServiceLoadBalancerArgs{
					TargetGroupArn: tg.Arn,
					ContainerName:  pulumi.String("plantcare"),
					ContainerPort:  pulumi.Int(8080),
				},
			},
		})
		if err != nil {
			return err
		}

		// ── Stack Outputs ─────────────────────────────────────────────────────
		ctx.Export("albDnsName", alb.DnsName)
		ctx.Export("ecrRepoUrl", repo.RepositoryUrl)
		return nil
	})
}
