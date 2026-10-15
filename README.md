# Media File Server & CDN

A scalable media processing and file-serving backend written in Go, backed by Amazon S3 storage and CloudFront CDN distribution. This project is a fork from [Learn File Servers and CDNs with S3 and CloudFront](https://www.boot.dev/courses/learn-file-servers-s3-cloudfront-golang) on [Boot.dev](boot.dev).


## Features

- Video and thumbnail upload processing (resizing, faststart encoding via `ffmpeg`).
- Secure object storage integration using Amazon S3.
- Global, low-latency caching and content delivery via Amazon CloudFront.
- Least-privilege AWS IAM policies and access control.


## Architecture Overview

1. **Client** uploads media files to the Go application server.
2. **Server** validates, processes, and transcodes media (e.g., aspect ratio checks, `moov` atom placement).
3. **Server** uploads the processed asset directly to a private **Amazon S3** bucket using authenticated AWS credentials.
4. **CloudFront** fronts the S3 bucket using Origin Access Control (OAC), serving cached content globally while keeping the bucket private.


## Getting started

### 1. Clone the repo

```
git clone https://github.com/paugomez86/tubely
```

### 2. Install Go toolchain

You may follow the instructions from the [official website](https://go.dev/doc/install).

### 3. Install dependencies

```
go mod download

sudo apt update
sudo apt install ffmpeg
sudo apt install sqlite3
```

### 4. Sync environment

```
go mod tidy
go mod verify
```

## AWS Setup Guide

To run this project locally or in production, configure the required AWS resources:

### 1. S3 Buckets
1. Open the [Amazon S3 Console](https://s3.console.aws.amazon.com/).
2. Create an S3 bucket (e.g., `my-bucket`).
   - **Region:** Choose your preferred region (e.g., `eu-south-2`).
   - **Public Access:** Keep **Block *all* public access** enabled. Content should only be accessible through CloudFront or authenticated API calls.
   - Leave default settings for encryption and versioning.

### 2. IAM User & Credentials
1. Open the [Amazon IAM Console](https://console.aws.amazon.com/iam/).
2. **Create a Custom Policy:**
   - Create a policy granting limited access to your bucket:
     - `s3:PutObject`
     - `s3:GetObject`
     - `s3:DeleteObject`
   - Scope the resource ARN strictly to your bucket contents: `arn:aws:s3:::<your-bucket-name>/*`.
3. **Create an IAM User / Group:**
   - Create a user (or group) dedicated to programmatic access (e.g., `server-app`).
   - Attach the custom policy created above.
4. **Generate Access Keys:**
   - Under the user's **Security credentials** tab, create an **Access Key**.
   - Save the **Access Key ID** and **Secret Access Key**.

### 3. AWS CLI
1. Install [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
2. Run `aws configure`
3. Enter your IAM **Access Key ID** and **Secret Access Key**
4. You can leave region and format blank

### 4. CloudFront Distribution (CDN)
1. Open the [CloudFront Console](https://console.aws.amazon.com/cloudfront/).
2. Create a distribution:
   - **Origin Domain:** Select your S3 bucket.
   - **Origin Access:** Choose **Origin access control settings (recommended)** and create an OAC.
   - Update the S3 bucket policy with the statement CloudFront provides to allow read access from the distribution.
3. Once deployed, note down the **Distribution domain name** (e.g., `d123456abcdef.cloudfront.net`).



## Local Development

### Prerequisites
- [Go](https://golang.org/) (version 1.20+ recommended)
- [FFmpeg](https://ffmpeg.org/) installed and available in your `PATH`
- An active AWS account

### Environment Configuration
Create a `.env` file in the root directory (you can use `.env.example` as boilerplate):

```
DB_PATH="./tubely.db"
JWT_SECRET="JKFNDKAJSDKFASFNJWIROIOTNKNFDSKNFD"
PLATFORM="dev"
FILEPATH_ROOT="./app"
ASSETS_ROOT="./assets"
S3_BUCKET="my-bucket"
S3_REGION="eu-south-2"
S3_CF_DISTRO="my-cdn-distro"
PORT="8091"