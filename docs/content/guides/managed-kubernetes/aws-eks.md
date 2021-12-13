# Capsule on AWS EKS
This is an example of how to install AWS EKS cluster and one user
manged by Capsule. It is based on [Using IAM Groups to manage Kubernetes access](https://www.eksworkshop.com/beginner/091_iam-groups/intro/)

Create EKS cluster:

```bash
export AWS_DEFAULT_REGION="eu-west-1"
export AWS_ACCESS_KEY_ID="xxxxx"
export AWS_SECRET_ACCESS_KEY="xxxxx"

eksctl create cluster \
--name=test-k8s \
--managed \
--node-type=t3.small \
--node-volume-size=20 \
--kubeconfig=kubeconfig.conf
```

Create AWS User `alice` using CloudFormation, create AWS access files and
kubeconfig for such user:

```bash
cat > cf.yml << EOF
Parameters:
  ClusterName:
    Type: String
Resources:
  UserAlice:
    Type: AWS::IAM::User
    Properties:
      UserName: !Sub "alice-${ClusterName}"
      Policies:
      - PolicyName: !Sub "alice-${ClusterName}-policy"
        PolicyDocument:
          Version: "2012-10-17"
          Statement:
          - Sid: AllowAssumeOrganizationAccountRole
            Effect: Allow
            Action: sts:AssumeRole
            Resource: !GetAtt RoleAlice.Arn
  AccessKeyAlice:
    Type: AWS::IAM::AccessKey
    Properties:
      UserName: !Ref UserAlice
  RoleAlice:
    Type: AWS::IAM::Role
    Properties:
      Description: !Sub "IAM role for the alice-${ClusterName} user"
      RoleName: !Sub "alice-${ClusterName}"
      AssumeRolePolicyDocument:
        Version: 2012-10-17
        Statement:
        - Effect: Allow
          Principal:
            AWS: !Sub "arn:aws:iam::${AWS::AccountId}:root"
          Action: sts:AssumeRole
Outputs:
  RoleAliceArn:
    Description: The ARN of the Alice IAM Role
    Value: !GetAtt RoleAlice.Arn
    Export:
      Name:
        Fn::Sub: "${AWS::StackName}-RoleAliceArn"
  AccessKeyAlice:
    Description: The AccessKey for Alice user
    Value: !Ref AccessKeyAlice
    Export:
      Name:
        Fn::Sub: "${AWS::StackName}-AccessKeyAlice"
  SecretAccessKeyAlice:
    Description: The SecretAccessKey for Alice user
    Value: !GetAtt AccessKeyAlice.SecretAccessKey
    Export:
      Name:
        Fn::Sub: "${AWS::StackName}-SecretAccessKeyAlice"
EOF

eval aws cloudformation deploy --capabilities CAPABILITY_NAMED_IAM \
  --parameter-overrides "ClusterName=test-k8s" \
  --stack-name "test-k8s-users" --template-file cf.yml

AWS_CLOUDFORMATION_DETAILS=$(aws cloudformation describe-stacks --stack-name "test-k8s-users")
ALICE_ROLE_ARN=$(echo "${AWS_CLOUDFORMATION_DETAILS}" | jq -r ".Stacks[0].Outputs[] | select(.OutputKey==\"RoleAliceArn\") .OutputValue")
ALICE_USER_ACCESSKEY=$(echo "${AWS_CLOUDFORMATION_DETAILS}" | jq -r ".Stacks[0].Outputs[] | select(.OutputKey==\"AccessKeyAlice\") .OutputValue")
ALICE_USER_SECRETACCESSKEY=$(echo "${AWS_CLOUDFORMATION_DETAILS}" | jq -r ".Stacks[0].Outputs[] | select(.OutputKey==\"SecretAccessKeyAlice\") .OutputValue")

eksctl create iamidentitymapping --cluster="test-k8s" --arn="${ALICE_ROLE_ARN}" --username alice --group capsule.clastix.io

cat > aws_config << EOF
[profile alice]
role_arn=${ALICE_ROLE_ARN}
source_profile=alice
EOF

cat > aws_credentials << EOF
[alice]
aws_access_key_id=${ALICE_USER_ACCESSKEY}
aws_secret_access_key=${ALICE_USER_SECRETACCESSKEY}
EOF

eksctl utils write-kubeconfig --cluster=test-k8s --kubeconfig="kubeconfig-alice.conf"
cat >> kubeconfig-alice.conf << EOF
      - name: AWS_PROFILE
        value: alice
      - name: AWS_CONFIG_FILE
        value: aws_config
      - name: AWS_SHARED_CREDENTIALS_FILE
        value: aws_credentials
EOF
```

Export "admin" kubeconfig to be able to install Capsule:

```bash
export KUBECONFIG=kubeconfig.conf
```

Install capsule from helm chart:

```bash
helm repo add clastix https://clastix.github.io/charts
helm upgrade --install --version 0.0.19 --namespace capsule-system --create-namespace capsule clastix/capsule
```

Use the default Tenant example:

```bash
kubectl apply -f https://raw.githubusercontent.com/clastix/capsule/master/config/samples/capsule_v1beta1_tenant.yaml
```

Based on the tenant configuration above the user `alice` should be able
to create namespace. Switch to a new terminal and try to create a namespace as user `alice`:

```bash
# Unset AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY if defined
unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
kubectl create namespace test --kubeconfig="kubeconfig-alice.conf"
```