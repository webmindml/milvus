# Copyright (C) 2019-2020 Zilliz. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance
# with the License. You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software distributed under the License
# is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express
# or implied. See the License for the specific language governing permissions and limitations under the License

milvus_add_pkg_config("milvus_common")

set(COMMON_SRC
        Schema.cpp
        SystemProperty.cpp
        Slice.cpp
        binary_set_c.cpp
        init_c.cpp
        Common.cpp
        RangeSearchHelper.cpp
        Tracer.cpp
        IndexMeta.cpp
        EasyAssert.cpp
)

add_library(milvus_common SHARED ${COMMON_SRC})

target_link_libraries(milvus_common
        milvus_config
        milvus_log
        milvus_proto
        yaml-cpp
        boost_bitset_ext
        simdjson
        opendal
        ${CONAN_LIBS}
        )

install(TARGETS milvus_common DESTINATION "${CMAKE_INSTALL_LIBDIR}")
