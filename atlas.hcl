env "local" {
  url = "postgres://postgres:postgres@localhost:5432/transformer?search_path=public&sslmode=disable"
  src = "file://schema/schema.sql"
  dev = "docker://postgres/15/dev?search_path=public"
}
