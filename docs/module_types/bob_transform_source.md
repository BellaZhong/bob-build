Module: bob_transform_source
============================

This target generates source code (headers or C files). A single
module generates output from each input (i.e. it runs the command
separately on each input file).

The command will be run once per source file with `$in` being the
path in `srcs` and `$out` being the path transformed
through the regular expression defined by `match` and `replace`.

See [https://golang.org/pkg/regexp/](https://golang.org/pkg/regexp/) for more information.
The working directory will be the source directory, and all paths
will be relative to the source directory if not else noted.

The module type is `bob_transform_source`.

## Full specification of `bob_transform_source` properties
For general common properties please
[check detailed documentation](common_module_properties.md).

For generate common properties please
[check detailed documentation](common_generate_module_properties.md).

```bp
bob_transform_source {
    name: "custom_name",
    srcs: ["src/a.cpp", "src/b.cpp", "src/common/*.cpp"],
    exclude_srcs: ["src/common/skip_this.cpp"],

    out: {
        match: "file_([0-9])+.cpp",
        replace: ["new_$1.o"],
        depfile: "$1.d",
        implicit_srcs: ["my_file.scu"],
    },

    enabled: false,
    build_by_default: true,

    add_to_alias: ["bob_alias.name"],

    cmd: "python ${tool} ${args} ${in}",
    tool: "my_script.py",

    host_bin: "clang-tblgen",
    tags: ["optional"],

    module_deps: ["bob_generate_source.name"],
    module_srcs: ["bob_generate_source.name"],

    args: ["-i graphic/ui.h"],

    console: true,

    export_gen_include_dirs: ["."],

    flag_defaults: ["bob_default.name"],

    target: "host",

    install_group: "bob_install_group.name",
    install_deps: ["bob_resource.name"],
    relative_install_path: "unit/objects",
    post_install_tool: "post_install.py",
    post_install_cmd: "${tool} ${out} ARGS...",
}
```

----
### **bob_transform_source.out.match** (required)
Regular expression to capture groups from srcs. There is support for catching groups.

----
### **bob_transform_source.out.replace** (required)
Names of outputs, which can use capture groups from match.
We can use catch groups e.g. `$1` for first group.

----
### **bob_transform_source.out.depfile** (optional)
Name of the dependency file (if needed), which can use
capture groups from match.

----
### **bob_transform_source.out.implicit_srcs** (optional)
List of implicit sources not described by the depfile.
