# Corpus fixture. Real Terraform, never applied — see the corpus README.
#
# Every block here is either a positive boundary (this fact must reach the bundle) or a
# negative one (this must reach nothing). The negatives are named in the README table.

terraform {
  required_version = ">= 1.9"

  required_providers {
    # Constrained here and configured below, which is how a well-formed configuration
    # spells one provider. Both must fold to one dependency: two would report a supply
    # chain larger than the one declared.
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.31"
    }
    # No `provider "random"` block, so this is the other half of the pair: a provider
    # constrained and never configured is still a plugin the run downloads.
    random = {
      source  = "hashicorp/random"
      version = "3.6.0"
    }
  }

  backend "s3" {
    # The value is the negative that matters most in this file. A backend block routinely
    # holds an access key beside the bucket, so the reader takes the backend's *name* and
    # stops. Neither of these strings may appear anywhere in the bundle.
    bucket = "corpus-state-do-not-publish"
    key    = "corpus/terraform.tfstate"
    region = "us-east-1"
  }
}

provider "aws" {
  region = "us-east-1"
}

# A local module: the one statement anywhere in this repository of which of its own
# directories the infrastructure is composed from. Resolved against this file's directory,
# so `./modules/queue` is `infra/modules/queue`, and it must become an edge rather than an
# external dependency page for a directory in this tree.
module "queue" {
  source = "./modules/queue"

  name = "corpus-events"
}

# A registry module, which is genuinely external and must stay that way. `terraform-aws-modules`
# is not a directory here, and Terraform's rule is that a source is local only if it starts
# with `./` or `../` — so a reader that guessed by looking for a matching directory would
# resolve this to nothing while a reader that guessed the other way would invent a dependency.
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.5.1"
}

# A workload: it runs something, so it is a service with a page. The image is a fact worth
# recording — it is the same claim a compose file's `image:` makes.
resource "aws_ecs_service" "worker" {
  name            = "corpus-worker"
  cluster         = aws_ecs_cluster.corpus.id
  desired_count   = 2
  task_definition = "corpus-worker:1"

  image = "docker.io/library/golang:1.26-alpine"
}

# Capacity that runs nothing by itself. It ends in `_cluster`, which is a workload suffix,
# and it is on the exceptions list — so this is the row that says the suffix rule is
# bounded by a list rather than trusted blindly.
resource "aws_ecs_cluster" "corpus" {
  name = "corpus"
}

# Wiring. Four resources a real configuration declares by the hundred, and none of them
# runs anything: a page for each would bury the one service above under the plumbing
# around it.
resource "aws_iam_role_policy_attachment" "worker" {
  role       = "corpus-worker"
  policy_arn = "arn:aws:iam::aws:policy/AmazonSQSFullAccess"
}

resource "aws_security_group_rule" "worker_egress" {
  type              = "egress"
  from_port         = 0
  to_port           = 0
  protocol          = "-1"
  security_group_id = "sg-corpus"
  cidr_blocks       = ["0.0.0.0/0"]
}

resource "aws_s3_bucket_policy" "assets" {
  bucket = "corpus-assets"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = "s3:GetObject"
    }]
  })
}

resource "aws_sns_topic_subscription" "alerts" {
  topic_arn = "arn:aws:sns:us-east-1:000000000000:corpus"
  protocol  = "email"
  endpoint  = "alerts@example.invalid"
}

# A secret store. Nothing runs here, and it is still a page: the resource *is* the named
# credential, so "where the credentials in this configuration live" is a thing a reader
# looks up, in the same way the `_bucket` and `_queue` suffixes already admit the places
# state lives. Attributing the reference to it is what makes it reachable at all — a
# reference naming a resource with no node reaches no page. The name arrives; the value
# beside it must not.
resource "aws_secretsmanager_secret" "db" {
  name = "corpus/db-credentials"
}

resource "aws_secretsmanager_secret_version" "db" {
  secret_id     = aws_secretsmanager_secret.db.id
  secret_string = "postgres://admin:hunter2-do-not-publish@db.corpus.invalid/corpus"
}

# A generated credential. Flagged for a different reason than the rest: whatever it
# produces lands in the state file in plain text, which is what a reader auditing this
# configuration needs to know.
resource "random_password" "session" {
  length  = 32
  special = true
}

# A data block reading something that exists elsewhere. Compute-shaped type, and still
# never a workload: it declares nothing this configuration owns. A `data` block reading a
# *secret* is read the same way — the reference stands, the page does not, because the
# thing it names is declared in another configuration.
data "aws_lambda_function" "existing" {
  function_name = "corpus-preexisting"
}

# A sensitive variable. The name reaches the bundle as a secret reference; the default
# beside it is the one place in this whole format where a live credential sits in plain
# text, and it must not.
variable "db_password" {
  type      = string
  sensitive = true
  default   = "s3cr3t-material-that-must-not-be-read"
}

# Sensitive by name rather than by declaration, which is the weaker signal and still a
# signal: plenty of real configurations never set `sensitive`.
variable "api_token" {
  type = string
}

# Not sensitive by either test, so it reaches nothing. Without it every variable in the
# file would be a secret reference and the two above would prove nothing.
variable "environment" {
  type    = string
  default = "staging"
}

# A sensitive output is a credential leaving this module for its caller, so it is recorded.
output "db_password_arn" {
  value     = aws_secretsmanager_secret.db.arn
  sensitive = true
}

# An ordinary output, read and deliberately dropped: Facts has no notion of an exported
# value, and inventing a field one reader writes and nothing reads would be worse than
# saying nothing.
output "queue_name" {
  value = module.queue.name
}

locals {
  # An object spanning lines, where a newline is an element separator and not whitespace.
  tags = {
    environment = var.environment
    managed_by  = "terraform"
  }
}
