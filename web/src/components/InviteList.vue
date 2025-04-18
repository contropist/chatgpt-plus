<template>
  <div class="invite-list" v-loading="loading">
    <el-row v-if="items.length > 0">
      <el-table
        :data="items"
        :row-key="(row) => row.id"
        table-layout="auto"
        border
      >
        <el-table-column prop="username" label="用户" />
        <el-table-column prop="invite_code" label="邀请码" />
        <el-table-column prop="remark" label="邀请奖励" />

        <el-table-column label="注册时间">
          <template #default="scope">
            <span>{{ dateFormat(scope.row["created_at"]) }}</span>
          </template>
        </el-table-column>
      </el-table>
    </el-row>
    <el-empty :image-size="100" :image="nodata" description="暂无数据" v-else />
    <div class="pagination">
      <el-pagination
        v-if="total > 0"
        background
        layout="total,prev, pager, next"
        :hide-on-single-page="true"
        v-model:current-page="page"
        v-model:page-size="pageSize"
        style="--el-pagination-button-bg-color: rgba(86, 86, 95, 0.2)"
        @current-change="fetchData()"
        :total="total"
      />
    </div>
  </div>
</template>

<script setup>
import nodata from "@/assets/img/no-data.png";

import { onMounted, ref } from "vue";
import { httpGet } from "@/utils/http";
import { ElMessage } from "element-plus";
import { dateFormat } from "@/utils/libs";
import Clipboard from "clipboard";

const items = ref([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);
const loading = ref(true);

onMounted(() => {
  fetchData();
  const clipboard = new Clipboard(".copy-order-no");
  clipboard.on("success", () => {
    ElMessage.success("复制成功！");
  });

  clipboard.on("error", () => {
    ElMessage.error("复制失败！");
  });
});

// 获取数据
const fetchData = () => {
  httpGet("/api/invite/list", { page: page.value, page_size: pageSize.value })
    .then((res) => {
      if (res.data) {
        items.value = res.data.items;
        total.value = res.data.total;
        page.value = res.data.page;
        pageSize.value = res.data.page_size;
      }
      loading.value = false;
    })
    .catch((e) => {
      ElMessage.error("获取数据失败：" + e.message);
    });
};
</script>

<style scoped lang="stylus">
.invite-list {
  .pagination {
    margin: 20px 0 0 0;
    display: flex;
    justify-content: center;
    width: 100%;
  }

  .copy-order-no {
    cursor pointer
    position relative
    left 6px
    top 2px
    color #20a0ff
  }
}
</style>
