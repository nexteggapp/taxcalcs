// Markdown files are bundled as text (see "loader" in angular.json).
declare module '*.md' {
  const text: string;
  export default text;
}
