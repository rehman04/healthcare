package(default_visibility = ["//visibility:public"])

licenses(["notice"])  # Apache 2.0

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "apply.go",
        "forseti.go",
        "migrations.go",
        "options.go",
        "terraform.go",
    ],
    data = [
        "@terraform_google_forseti//:all_files",
    ],
    importpath = "github.com/GoogleCloudPlatform/healthcare/deploy/apply",
    deps = [
        "//config:go_default_library",
        "//config/tfconfig:go_default_library",
        "//runner:go_default_library",
        "//terraform:go_default_library",
        "@com_github_imdario_mergo//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = [
        "forseti_test.go",
        "migrations_test.go",
        "terraform_test.go",
    ],
    embed = [":go_default_library"],
    # Override default run dir to make it easier to find test files.
    rundir = ".",
    deps = [
        "//config:go_default_library",
        "//runner:go_default_library",
        "//terraform:go_default_library",
        "//testconf:go_default_library",
        "@com_github_google_cmp//cmp:go_default_library",
        "@in_ghodss_yaml//:go_default_library",
    ],
)
