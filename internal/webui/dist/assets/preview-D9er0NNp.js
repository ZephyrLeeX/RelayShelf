import{n as t}from"./index-D7I-dQdl.js";/**
 * @license @lucide/vue v1.37.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const i=[["path",{d:"M12 15V3",key:"m9g1x1"}],["path",{d:"M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4",key:"ih7n3h"}],["path",{d:"m7 10 5 5 5-5",key:"brsn70"}]],d=t("download",i),o=new Set(["image/jpeg","image/png","image/gif","image/webp"]),n=new Set(["text/html","application/xhtml+xml","image/svg+xml","application/xml","text/xml"]),p=new Set(["audio/mpeg","audio/mp4","audio/ogg","audio/wav","audio/webm","video/mp4","video/webm","video/ogg","video/quicktime"]);function m(e){const a=e.detectedMime.toLowerCase();return o.has(a)?"image":a==="application/pdf"?"pdf":p.has(a)?a.startsWith("audio/")?"audio":"video":!n.has(a)&&(a.startsWith("text/")||["application/json","application/yaml","application/x-yaml","application/sql","application/javascript"].includes(a))?"text":"download"}const c=e=>`/api/v1/attachments/${encodeURIComponent(e)}/download`,l=e=>`/api/v1/attachments/${encodeURIComponent(e)}/preview`;export{d as D,l as a,c as d,m as p,o as s};
