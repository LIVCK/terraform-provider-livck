terraform {
  required_providers {
    livck = {
      source  = "livck/livck"
      version = "~> 0.1"
    }
  }
}

# The token comes from the LIVCK_API_TOKEN environment variable.
# Mint one in the console: Settings > API Tokens (plan Team or higher).
provider "livck" {}
