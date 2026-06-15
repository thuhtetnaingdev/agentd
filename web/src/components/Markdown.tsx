import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

interface MarkdownProps {
  content: string;
}

export default function Markdown({ content }: MarkdownProps) {
  return (
    <div className="prose prose-sm prose-invert max-w-none break-words">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // Style code blocks
          pre: ({ children }) => (
            <pre className="bg-background border border-border rounded-lg p-3 overflow-x-auto my-2 text-xs">
              {children}
            </pre>
          ),
          code: ({ className, children, ...props }) => {
            const isInline = !className;
            return isInline ? (
              <code
                className="bg-secondary px-1.5 py-0.5 rounded text-[11px] font-mono"
                {...props}
              >
                {children}
              </code>
            ) : (
              <code className="text-xs font-mono" {...props}>
                {children}
              </code>
            );
          },
          // Style tables
          table: ({ children }) => (
            <div className="overflow-x-auto my-2">
              <table className="min-w-full border-collapse text-xs">
                {children}
              </table>
            </div>
          ),
          th: ({ children }) => (
            <th className="border border-border px-3 py-1.5 text-left font-semibold bg-secondary/50">
              {children}
            </th>
          ),
          td: ({ children }) => (
            <td className="border border-border px-3 py-1.5">{children}</td>
          ),
          // Style lists
          ul: ({ children }) => (
            <ul className="list-disc list-inside my-1 space-y-0.5">{children}</ul>
          ),
          ol: ({ children }) => (
            <ol className="list-decimal list-inside my-1 space-y-0.5">{children}</ol>
          ),
          // Style links
          a: ({ href, children }) => (
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary underline hover:text-primary/80"
            >
              {children}
            </a>
          ),
          // Style headings
          h1: ({ children }) => (
            <h1 className="text-base font-bold mt-3 mb-1">{children}</h1>
          ),
          h2: ({ children }) => (
            <h2 className="text-sm font-bold mt-3 mb-1">{children}</h2>
          ),
          h3: ({ children }) => (
            <h3 className="text-sm font-semibold mt-2 mb-1">{children}</h3>
          ),
          // Style paragraphs
          p: ({ children }) => (
            <p className="my-1 leading-relaxed">{children}</p>
          ),
          // Style horizontal rules
          hr: () => <hr className="border-border my-3" />,
          // Style blockquotes
          blockquote: ({ children }) => (
            <blockquote className="border-l-2 border-primary/30 pl-3 my-2 text-muted-foreground italic">
              {children}
            </blockquote>
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
