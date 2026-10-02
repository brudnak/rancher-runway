// Package awspricing queries the AWS price catalog using the existing environment credentials.
package awspricing

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	elbv2Types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"math"
	"os"
	"strings"
)

func EC2HourlyUSD(region, instanceType string) (float64, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				os.Getenv("AWS_SESSION_TOKEN"),
			),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to load AWS pricing config: %w", err)
	}

	pricingClient := pricing.NewFromConfig(cfg)
	location, err := awsPricingLocation(region)
	if err != nil {
		return 0, err
	}

	output, err := pricingClient.GetProducts(context.Background(), &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		MaxResults:  aws.Int32(100),
		Filters: []pricingTypes.Filter{
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("location"), Value: aws.String(location)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("instanceType"), Value: aws.String(instanceType)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("operatingSystem"), Value: aws.String("Linux")},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("tenancy"), Value: aws.String("Shared")},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("preInstalledSw"), Value: aws.String("NA")},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("capacitystatus"), Value: aws.String("Used")},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to query EC2 pricing: %w", err)
	}

	return extractUSDPriceFromPricingResult(output.PriceList)
}

func EBSMonthlyPerGiBUSD(region, volumeType string) (float64, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				os.Getenv("AWS_SESSION_TOKEN"),
			),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to load AWS pricing config: %w", err)
	}

	pricingClient := pricing.NewFromConfig(cfg)
	location, err := awsPricingLocation(region)
	if err != nil {
		return 0, err
	}

	output, err := pricingClient.GetProducts(context.Background(), &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		MaxResults:  aws.Int32(100),
		Filters: []pricingTypes.Filter{
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("location"), Value: aws.String(location)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("productFamily"), Value: aws.String("Storage")},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("volumeApiName"), Value: aws.String(volumeType)},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to query EBS pricing: %w", err)
	}

	return extractUSDPriceFromPricingResult(output.PriceList)
}

func RDSHourlyUSD(region, instanceClass, engine string) (float64, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				os.Getenv("AWS_SESSION_TOKEN"),
			),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to load AWS pricing config: %w", err)
	}

	location, err := awsPricingLocation(region)
	if err != nil {
		return 0, err
	}
	databaseEngine := "Aurora MySQL"
	if !strings.Contains(strings.ToLower(engine), "aurora") {
		databaseEngine = "MySQL"
	}

	output, err := pricing.NewFromConfig(cfg).GetProducts(context.Background(), &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonRDS"),
		MaxResults:  aws.Int32(100),
		Filters: []pricingTypes.Filter{
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("location"), Value: aws.String(location)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("instanceType"), Value: aws.String(instanceClass)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("databaseEngine"), Value: aws.String(databaseEngine)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("deploymentOption"), Value: aws.String("Single-AZ")},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to query RDS pricing: %w", err)
	}

	return extractUSDPriceFromPricingResult(output.PriceList)
}

func LoadBalancerHourlyUSD(region string, lbType elbv2Types.LoadBalancerTypeEnum) (float64, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				os.Getenv("AWS_SESSION_TOKEN"),
			),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to load AWS pricing config: %w", err)
	}

	location, err := awsPricingLocation(region)
	if err != nil {
		return 0, err
	}

	operation := "LoadBalancing:Application"
	groupDescription := "LoadBalancer hourly usage by Application Load Balancer"
	switch lbType {
	case elbv2Types.LoadBalancerTypeEnumNetwork:
		operation = "LoadBalancing:Network"
		groupDescription = "LoadBalancer hourly usage by Network Load Balancer"
	case elbv2Types.LoadBalancerTypeEnumGateway:
		operation = "LoadBalancing:Gateway"
		groupDescription = "LoadBalancer hourly usage by Gateway Load Balancer"
	}

	output, err := pricing.NewFromConfig(cfg).GetProducts(context.Background(), &pricing.GetProductsInput{
		ServiceCode: aws.String("AWSELB"),
		MaxResults:  aws.Int32(100),
		Filters: []pricingTypes.Filter{
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("location"), Value: aws.String(location)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("operation"), Value: aws.String(operation)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("groupDescription"), Value: aws.String(groupDescription)},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to query ELB pricing: %w", err)
	}

	return extractUSDPriceFromPricingResult(output.PriceList)
}

func extractUSDPriceFromPricingResult(priceList []string) (float64, error) {
	type pricingDocument struct {
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					PricePerUnit map[string]string `json:"pricePerUnit"`
				} `json:"priceDimensions"`
			} `json:"OnDemand"`
		} `json:"terms"`
	}

	bestPrice := math.MaxFloat64
	for _, item := range priceList {
		var doc pricingDocument
		if err := json.Unmarshal([]byte(item), &doc); err != nil {
			continue
		}

		for _, offer := range doc.Terms.OnDemand {
			for _, dimension := range offer.PriceDimensions {
				usdValue := strings.TrimSpace(dimension.PricePerUnit["USD"])
				if usdValue == "" {
					continue
				}
				var price float64
				if _, err := fmt.Sscanf(usdValue, "%f", &price); err != nil {
					continue
				}
				if price > 0 && price < bestPrice {
					bestPrice = price
				}
			}
		}
	}

	if bestPrice == math.MaxFloat64 {
		return 0, fmt.Errorf("no USD price found in pricing response")
	}

	return bestPrice, nil
}

func awsPricingLocation(region string) (string, error) {
	locations := map[string]string{
		"us-east-1": "US East (N. Virginia)",
		"us-east-2": "US East (Ohio)",
		"us-west-1": "US West (N. California)",
		"us-west-2": "US West (Oregon)",
	}
	location := locations[region]
	if location == "" {
		return "", fmt.Errorf("no AWS pricing location mapping configured for region %s", region)
	}
	return location, nil
}
