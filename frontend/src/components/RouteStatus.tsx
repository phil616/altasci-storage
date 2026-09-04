import { Button, Result, Spin } from "antd";
import { isRouteErrorResponse, useRouteError } from "react-router-dom";

export function RouteLoading() {
  return <div className="route-status"><Spin size="large" description="正在加载页面" /></div>;
}

export function RouteErrorPage() {
  const error = useRouteError();
  const detail = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : error instanceof Error ? error.message : "页面发生未知错误";
  return (
    <div className="route-status">
      <Result
        status="error"
        title="页面加载失败"
        subTitle={detail}
        extra={<Button type="primary" onClick={() => window.location.reload()}>重新加载</Button>}
      />
    </div>
  );
}
