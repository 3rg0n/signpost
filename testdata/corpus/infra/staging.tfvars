# Corpus fixture. Values, not structure — a .tfvars file has no blocks at all.
#
# This is the single most likely file in a Terraform repository to hold a live credential,
# which is why the reader takes names and stops. Every value below is a negative boundary:
# none of them may appear anywhere in the bundle.

environment = "staging"
region      = "us-east-1"

db_password = "tfvars-value-must-never-be-read"
api_token   = "tfvars-token-must-never-be-read"

# Not credential-shaped by name, so it is not recorded at all. Without it the reader could
# be recording every assignment in the file and the two above would prove nothing.
instance_count = 3
