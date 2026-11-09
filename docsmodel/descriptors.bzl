"""A descriptor set that keeps the .proto files' comments."""

load("@protobuf//bazel/common:proto_info.bzl", "ProtoInfo")

def _import_path(f, root):
    p = f.path
    if root and p.startswith(root + "/"):
        return p[len(root) + 1:]
    return p

def _impl(ctx):
    out = ctx.actions.declare_file(ctx.label.name + ".binpb")
    args = ctx.actions.args()
    args.add("--include_imports")
    args.add("--include_source_info")
    args.add("--descriptor_set_out", out)
    sources = []
    paths = []
    for dep in ctx.attr.deps:
        info = dep[ProtoInfo]
        sources.append(info.transitive_sources)
        paths.append(info.transitive_proto_path)
    args.add_all(depset(transitive = paths), format_each = "-I%s")
    args.add("-I.")
    for dep in ctx.attr.deps:
        info = dep[ProtoInfo]
        args.add_all([_import_path(f, info.proto_source_root) for f in info.direct_sources])
    ctx.actions.run(
        executable = ctx.executable._protoc,
        arguments = [args],
        inputs = depset(transitive = sources),
        outputs = [out],
        mnemonic = "DescriptorSetWithComments",
    )
    return [DefaultInfo(files = depset([out]))]

descriptor_set_with_comments = rule(
    implementation = _impl,
    doc = """Writes a FileDescriptorSet of deps and their imports, with the
    comments (source info) that proto_library's own descriptor sets leave out.""",
    attrs = {
        "deps": attr.label_list(providers = [ProtoInfo]),
        "_protoc": attr.label(
            default = "@protobuf//:protoc",
            executable = True,
            cfg = "exec",
        ),
    },
)
