cmake_minimum_required(VERSION 3.11)
project(rnapp)

set(RNOH_APP_DIR "${CMAKE_CURRENT_SOURCE_DIR}")
set(NODE_MODULES "${CMAKE_CURRENT_SOURCE_DIR}/../../../../../node_modules")
# release har 把头文件与 cmake 放在 include/ 下（源码 har 才有的 src/main/cpp
# 在 release 包里不存在，所以这里既不能 add_subdirectory 也不能指回源码路径）。
set(RNOH_CPP_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../../../../oh_modules/@rnoh/react-native-openharmony/include")
set(RNOH_GENERATED_DIR "${CMAKE_CURRENT_SOURCE_DIR}/generated")
set(LOG_VERBOSITY_LEVEL 1)
set(CMAKE_ASM_FLAGS "-Wno-error=unused-command-line-argument -Qunused-arguments")
set(CMAKE_CXX_FLAGS "-fstack-protector-strong -Wl,-z,relro,-z,now,-z,noexecstack -s -fPIE -pie")
set(WITH_HITRACE_SYSTRACE ON)
set(WITH_HITRACE_REACT_MARKER ON)
set(OH_MODULES_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../../../../oh_modules") # required by 'autolink_libraries'

# 预编译 .so 的导入目标由 ohpm 安装时生成的 summary.cmake 提供，hvigor 通过
# -DPACKAGE_FIND_FILE 传进来；这里 include 官方 cmake 片段即可拿到 rnoh 目标。
include("${RNOH_CPP_DIR}/react-native-harmony.cmake")

# ohpm 生成的导入目标把 .ohpm 虚拟 store 的绝对路径写进了 include 目录（194
# 字符），加上头文件名就超过 Windows 的 260 上限，ninja 记录依赖时直接失败：
#   Stat(.../include/RNOHCorePackage/ComponentBinders/DrawerLayoutAndroidJSIBinder.h):
#   Filename longer than 260 characters
# oh_modules/@rnoh/react-native-openharmony 是指向 store 的目录联接：内容完全
# 一样，路径却短得多。这里把导入目标里的 store 绝对路径换回联接路径。
get_property(RNOH_IMPORTED_TARGETS DIRECTORY PROPERTY IMPORTED_TARGETS)
foreach(RNOH_TARGET IN LISTS RNOH_IMPORTED_TARGETS)
    if(RNOH_TARGET MATCHES "^react-native-openharmony::")
        get_target_property(RNOH_INCLUDES ${RNOH_TARGET} INTERFACE_INCLUDE_DIRECTORIES)
        if(RNOH_INCLUDES)
            string(REGEX REPLACE "/oh_modules/\\.ohpm/[^/]+/oh_modules/" "/oh_modules/" RNOH_INCLUDES "${RNOH_INCLUDES}")
            set_target_properties(${RNOH_TARGET} PROPERTIES INTERFACE_INCLUDE_DIRECTORIES "${RNOH_INCLUDES}")
        endif()
        get_target_property(RNOH_LOCATION ${RNOH_TARGET} IMPORTED_LOCATION)
        if(RNOH_LOCATION)
            string(REGEX REPLACE "/oh_modules/\\.ohpm/[^/]+/oh_modules/" "/oh_modules/" RNOH_LOCATION "${RNOH_LOCATION}")
            set_target_properties(${RNOH_TARGET} PROPERTIES IMPORTED_LOCATION "${RNOH_LOCATION}")
        endif()
    endif()
endforeach()

include("${CMAKE_CURRENT_SOURCE_DIR}/autolinking.cmake")

file(GLOB GENERATED_CPP_FILES "${CMAKE_CURRENT_SOURCE_DIR}/generated/*.cpp") # this line is needed by codegen v1
add_library(rnoh_app SHARED
    ${GENERATED_CPP_FILES}
    "./PackageProvider.cpp"
    "${RNOH_CPP_DIR}/RNOHAppNapiBridge.cpp"
)
target_link_libraries(rnoh_app PUBLIC rnoh)

autolink_libraries(rnoh_app)
