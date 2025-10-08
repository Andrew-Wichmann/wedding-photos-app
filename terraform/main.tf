terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

resource "aws_s3_bucket" "photos" {
  bucket = "wedding-photos-${random_string.bucket_suffix.result}"
}

resource "aws_s3_bucket_cors_configuration" "photos" {
  bucket = aws_s3_bucket.photos.id

  cors_rule {
    allowed_headers = ["*"]
    allowed_methods = ["GET", "PUT", "POST"]
    allowed_origins = ["*"]
    expose_headers  = ["ETag"]
    max_age_seconds = 3600
  }
}

resource "random_string" "bucket_suffix" {
  length  = 8
  special = false
  upper   = false
}

# IAM role for Lambda
resource "aws_iam_role" "lambda_role" {
  name = "wedding-photo-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })
}

# Attach basic execution policy
resource "aws_iam_role_policy_attachment" "lambda_basic" {
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
  role       = aws_iam_role.lambda_role.name
}

# S3 permissions for Lambda
resource "aws_iam_role_policy" "lambda_s3_policy" {
  name = "lambda-s3-policy"
  role = aws_iam_role.lambda_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject"
        ]
        Resource = "${aws_s3_bucket.photos.arn}/*"
      },
      {
        Effect = "Allow"
        Action = [
          "s3:ListBucket"
        ]
        Resource = aws_s3_bucket.photos.arn
      }
    ]
  })
}

# Lambda function
resource "aws_lambda_function" "wedding_app" {
  filename         = "../lambda/main.zip"
  function_name    = "wedding-photos-app"
  role            = aws_iam_role.lambda_role.arn
  handler         = "main"
  runtime         = "go1.x"
  
  source_code_hash = filebase64sha256("../lambda/main.zip")

  environment {
    variables = {
      S3_BUCKET = aws_s3_bucket.photos.bucket
    }
  }
}

# Lambda Function URL
resource "aws_lambda_function_url" "wedding_app" {
  function_name      = aws_lambda_function.wedding_app.function_name
  authorization_type = "NONE"

  cors {
    allow_credentials = false
    allow_origins     = ["*"]
    allow_methods     = ["*"]
    allow_headers     = ["date", "keep-alive", "content-type"]
    expose_headers    = ["date", "keep-alive"]
    max_age          = 86400
  }
}

# Data source for existing Route 53 zone
data "aws_route53_zone" "main" {
  name = "awichmann.com"
}

# ACM Certificate
resource "aws_acm_certificate" "wedding" {
  domain_name       = "wedding.awichmann.com"
  validation_method = "DNS"
  
  lifecycle {
    create_before_destroy = true
  }
}

# Route 53 validation
resource "aws_route53_record" "wedding_validation" {
  for_each = {
    for dvo in aws_acm_certificate.wedding.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  }

  zone_id = data.aws_route53_zone.main.zone_id
  name    = each.value.name
  type    = each.value.type
  records = [each.value.record]
  ttl     = 60
}

# CloudFront distribution
resource "aws_cloudfront_distribution" "wedding" {
  origin {
    domain_name = trimprefix(trimsuffix(aws_lambda_function_url.wedding_app.function_url, "/"), "https://")
    origin_id   = "lambda-function-url"
    
    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  enabled = true
  aliases = ["wedding.awichmann.com"]

  default_cache_behavior {
    allowed_methods               = ["DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"]
    cached_methods                = ["GET", "HEAD"]
    target_origin_id              = "lambda-function-url"
    compress                      = true
    viewer_protocol_policy        = "redirect-to-https"
    origin_request_policy_id      = aws_cloudfront_origin_request_policy.lambda_function_url.id
    cache_policy_id               = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad" # Managed-CachingDisabled
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate.wedding.arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }

  depends_on = [aws_acm_certificate_validation.wedding]
}

# Custom origin request policy for Lambda Function URLs
resource "aws_cloudfront_origin_request_policy" "lambda_function_url" {
  name = "lambda-function-url-policy"
  
  cookies_config {
    cookie_behavior = "all"
  }
  
  headers_config {
    header_behavior = "whitelist"
    headers {
      items = ["Accept", "Accept-Language", "Content-Type", "User-Agent", "Referer"]
    }
  }
  
  query_strings_config {
    query_string_behavior = "all"
  }
}

# Certificate validation
resource "aws_acm_certificate_validation" "wedding" {
  certificate_arn         = aws_acm_certificate.wedding.arn
  validation_record_fqdns = [for record in aws_route53_record.wedding_validation : record.fqdn]
}

# Route 53 record pointing to CloudFront
resource "aws_route53_record" "wedding" {
  zone_id = data.aws_route53_zone.main.zone_id
  name    = "wedding"
  type    = "A"
  
  alias {
    name                   = aws_cloudfront_distribution.wedding.domain_name
    zone_id                = aws_cloudfront_distribution.wedding.hosted_zone_id
    evaluate_target_health = false
  }
}
