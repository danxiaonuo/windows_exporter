#!/bin/bash
set -e

# =============================
# 自动注册 my_collectors 采集器脚本
# 适用于 windows_exporter 项目
# 运行环境：Linux (在项目根目录下运行)
# 说明：为避免 Go 包循环依赖，主项目不应 import my_collectors 包，
# 只在 main.go 匿名 import 触发插件注册。
# =============================

COLLECTORS_DIR="my_collectors"  # 源采集器目录（相对项目根目录）
COLLECTOR_GO="windows_exporter/pkg/collector/collect.go"  # 主注册文件（相对项目根目录）
COLLECTION_GO="windows_exporter/pkg/collector/collection.go"  # Collection 初始化文件
MAIN_GO="windows_exporter/cmd/windows_exporter/main.go"  # 主程序入口
TMP_IMPORT_GO="${COLLECTOR_GO}.import.tmp"  # 临时文件
WIN_EXPORTER_MY_COLLECTORS="windows_exporter/my_collectors"  # 目标目录（相对项目根目录）
GIT_COMMIT_MSG="auto: register new collectors from my_collectors"

# 0. 如主注册文件未定义 RegisterCollector，则自动插入注册表和注册函数（插入到 import 语句块之后），并插入日志
if ! grep -q "func RegisterCollector" "$COLLECTOR_GO"; then
    awk '
    BEGIN {import_start=0; import_end=0}
    {
        print
        if ($1=="import" && $2=="(") {import_start=NR}
        if ($0==")" && import_start && !import_end) {import_end=NR}
        if (import_end && NR==import_end) {
            print ""
            print "// 全局自定义采集器注册表"
            print "var CustomCollectors = map[string]func() Collector{}"
            print ""
            print "// 供自动注册脚本调用"
            print "func RegisterCollector(name string, factory func() Collector) {"
            print "    fmt.Println(\"registerCollector called for:\", name)"  # 日志
            print "    if _, exists := CustomCollectors[name]; !exists {"
            print "        CustomCollectors[name] = factory"
            print "    }"
            print "}"
            print ""
        }
    }
    ' "$COLLECTOR_GO" > "${COLLECTOR_GO}.tmp"
    mv "${COLLECTOR_GO}.tmp" "$COLLECTOR_GO"
fi

# 0.5. 在 Collection 初始化函数体内插入 CustomCollectors 合并到 collectors 的代码（防止重复插入），并插入日志
for funcname in NewWithFlags NewWithConfig New; do
    if ! grep -q "for name, factory := range CustomCollectors {" "$COLLECTION_GO"; then
        awk -v fname="$funcname" '
        BEGIN {in_func=0; brace_count=0}
        {
            print
            if ($0 ~ "^func " fname "\\(") {in_func=1}
            if (in_func && $0 ~ /collectors *:=/ && brace_count==0) {
                print "    fmt.Println(\"CustomCollectors at merge:\", CustomCollectors)"  # 日志
                print "    // 自动合并自定义采集器"
                print "    for name, factory := range CustomCollectors {"
                print "        collectors[name] = factory()"
                print "    }"
            }
            if (in_func) {
                brace_count += gsub(/{/, "{")
                brace_count -= gsub(/}/, "}")
                if (brace_count == 0) in_func=0
            }
        }
        ' "$COLLECTION_GO" > "${COLLECTION_GO}.tmp"
        mv "${COLLECTION_GO}.tmp" "$COLLECTION_GO"
    fi
done

# 0.6. 在 main.go 的 import 块内部插入 _ "github.com/prometheus-community/windows_exporter/my_collectors"，防止重复插入
if ! grep -q 'github.com/prometheus-community/windows_exporter/my_collectors' "$MAIN_GO"; then
    awk '
    BEGIN {importing=0; inserted=0}
    /^import \($/ {print; importing=1; next}
    importing && !inserted && $0 !~ /^ *$/ {
        print "\t_ \"github.com/prometheus-community/windows_exporter/my_collectors\""
        inserted=1
    }
    {print}
    /^ *\)/ && importing {importing=0}
    ' "$MAIN_GO" > "${MAIN_GO}.tmp"
    mv "${MAIN_GO}.tmp" "$MAIN_GO"
fi

# 1. 自动复制 my_collectors 目录下所有 .go 文件到 windows_exporter/my_collectors
mkdir -p "$WIN_EXPORTER_MY_COLLECTORS"
find "$COLLECTORS_DIR" -maxdepth 1 -name '*.go' -exec cp {} "$WIN_EXPORTER_MY_COLLECTORS/" \;

# 2. 工具函数：下划线转驼峰
function to_camel_case() {
    local input="$1"
    local output=""
    IFS='_' read -ra parts <<< "$input"
    for part in "${parts[@]}"; do
        output="${output}$(tr '[:lower:]' '[:upper:]' <<< ${part:0:1})${part:1}"
    done
    echo "$output"
}

# 3. 生成 import 和注册代码（去重）
IMPORTS_SET=()
REGISTERS_SET=()
IMPORTS=""
REGISTERS=""

for file in ${COLLECTORS_DIR}/*_collector.go; do
    [ -e "$file" ] || continue
    base=$(basename "$file" .go)
    collector_name="${base%_collector}"
    camel_collector_name=$(to_camel_case "$collector_name")
    ctor="New${camel_collector_name}Collector"
    import_line="\t\"github.com/prometheus-community/windows_exporter/my_collectors\""  # 使用 go.mod 的 module 路径
    register_line="\tRegisterCollector(\"${collector_name}\", func() Collector { return my_collectors.${ctor}() })"
    # 去重
    if [[ ! " ${IMPORTS_SET[*]} " =~ "${import_line}" ]]; then
        IMPORTS_SET+=("${import_line}")
        IMPORTS+="${import_line}\n"
    fi
    if [[ ! " ${REGISTERS_SET[*]} " =~ "${register_line}" ]]; then
        REGISTERS_SET+=("${register_line}")
        REGISTERS+="${register_line}\n"
    fi
done

# 4. 替换 import 块（精确定位起止行号，只替换import块内容，其他内容全部保留）
IMPORT_START=$(awk '/^import \(/ {print NR; exit}' "$COLLECTOR_GO")
IMPORT_END=$(awk 'NR>'"$IMPORT_START"' && /^\)/ {print NR; exit}' "$COLLECTOR_GO")

if [[ -z "$IMPORT_START" || -z "$IMPORT_END" ]]; then
    echo "未找到 import 块，脚本终止。"
    exit 1
fi

{
    awk "NR < $IMPORT_START" "$COLLECTOR_GO"
    echo "import ("
    echo -e "$IMPORTS"
    # 保留原有 import，去除重复的 my_collectors import
    awk "NR > $IMPORT_START && NR < $IMPORT_END" "$COLLECTOR_GO" | grep -v 'my_collectors' | grep -v 'github.com.prometheus-community.windows_exporter.my_collectors' | grep -v 'github.com/prometheus-community/windows_exporter/my_collectors'
    echo ")"
    awk "NR > $IMPORT_END" "$COLLECTOR_GO"
} > "$TMP_IMPORT_GO"

# 5. 替换或追加 init 块（只替换 init 块内容，其他内容全部保留）
if grep -q '^func init() {' "$TMP_IMPORT_GO"; then
    # 替换已有的 init 块
    awk -v registers="$REGISTERS" '
        BEGIN {in_init=0}
        /^func init\(\) \{/ {print "func init() {"; print registers; in_init=1; next}
        /^[ \t]*RegisterCollector\(/ {next}
        /^\}/ && in_init {in_init=0; print "}"; next}
        {if (!in_init) print}
    ' "$TMP_IMPORT_GO" > "$COLLECTOR_GO"
else
    # 没有 init 块则追加
    cat "$TMP_IMPORT_GO" > "$COLLECTOR_GO"
    echo -e "\nfunc init() {\n$REGISTERS}" >> "$COLLECTOR_GO"
fi

# 6. 清理临时文件
rm -f "$TMP_IMPORT_GO"

# 7. （可选）自动 git add & commit
# git add "$COLLECTOR_GO" "$WIN_EXPORTER_MY_COLLECTORS"/*.go
# git commit -m "$GIT_COMMIT_MSG"

# =============================
# 脚本结束
# ============================= 

# 8. 强制删除 collect.go 中所有 my_collectors 相关 import，彻底避免循环依赖
sed -i '/my_collectors/d' windows_exporter/pkg/collector/collect.go
# 9. 自动清理 my_collectors 目录下所有 .go 文件中的不可见字符（如 ESC/U+001B），合并为一行
sed -i 's/[^[:print:]\t]//g' windows_exporter/my_collectors/*.go

# 10. 优化：插入 CustomCollectors 合并逻辑前，先清理旧的合并片段，防止重复
sed -i '/CustomCollectors at merge/,+4d' windows_exporter/pkg/collector/collection.go
for funcname in NewWithFlags NewWithConfig; do
    awk -v fname="$funcname" '
    BEGIN {in_func=0; brace_count=0; inserted=0}
    {
        if ($0 ~ "^func " fname "\\(") {in_func=1}
        if (in_func && $0 ~ /collectors *:=/ && brace_count==0 && !inserted) {
            print
            print "    // 自动合并自定义采集器"
            print "    fmt.Println(\"CustomCollectors at merge:\", CustomCollectors)"  # 日志
            print "    for name, factory := range CustomCollectors {"
            print "        collectors[name] = factory()"
            print "    }"
            inserted=1
            next
        }
        if (in_func && $0 ~ /^\s*return / && inserted==0) {
            # 如果没找到 collectors 初始化，也在 return 前插入
            print "    // 自动合并自定义采集器 (return 前)"
            print "    fmt.Println(\"CustomCollectors at merge:\", CustomCollectors)"
            print "    for name, factory := range CustomCollectors {"
            print "        collectors[name] = factory()"
            print "    }"
            inserted=1
        }
        print
        if (in_func) {
            brace_count += gsub(/{/, "{")
            brace_count -= gsub(/}/, "}")
            if (brace_count == 0) in_func=0
        }
    }
    ' "$COLLECTION_GO" > "${COLLECTION_GO}.tmp"
    mv "${COLLECTION_GO}.tmp" "$COLLECTION_GO"
done
# New(collectors) 由 NewWithFlags/NewWithConfig 调用，无需重复插入。
