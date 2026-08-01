// A Next.js catch-all route: brackets and dots in one segment.
import React from "react";

export default function BlogPage({ params }: { params: { rest: string[] } }) {
  return <p>{params.rest.join("/")}</p>;
}
