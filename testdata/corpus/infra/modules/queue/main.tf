# Corpus fixture. The local module `infra/main.tf` calls, so the call has a real target.
#
# It also carries the parser cases a naive brace count gets wrong. Each of them is a brace
# that is not structure, and the observable is not a diagnostic: a miscount silently
# reparents the rest of the file, so the resources below would stop being top-level and
# the service page would vanish with no error anywhere.

variable "name" {
  type = string
}

resource "aws_sqs_queue" "events" {
  # A brace inside an interpolated string, with a nested quoted string inside the
  # interpolation. A scanner that ends the string at the first `"` ends it inside the
  # `${...}` and inverts every quote in the file from here down.
  name = "${format("%s{}", var.name)}-events"

  # A brace inside a plain string.
  policy = "{}"

  tags = {
    # A newline separating object elements, which HCL treats exactly as it treats a comma.
    Name    = var.name
    Managed = "terraform"
  }
}

resource "aws_sqs_queue_policy" "events" {
  # Ends in `_policy`, which is not a workload suffix, so this is a resource read and not
  # recorded even without an exceptions entry.
  queue_url = aws_sqs_queue.events.id

  policy = <<-EOT
    {
      "Version": "2012-10-17",
      "Statement": [{ "Effect": "Allow", "Action": "sqs:SendMessage" }]
    }
  EOT
}

resource "aws_lambda_function" "consumer" {
  function_name = "${var.name}-consumer" # a comment after a value, holding a } brace
  runtime       = "provided.al2023"
  handler       = "bootstrap"

  /* A block comment spanning lines, with a } in it.
     Still a comment } */

  environment {
    variables = {
      QUEUE_NAME = var.name
    }
  }
}

output "name" {
  value = aws_sqs_queue.events.name
}
