# Loki UI

The Loki UI is an operational dashboard for managing and operating Grafana Loki clusters. It provides a comprehensive set of tools for cluster administration, operational tasks, and troubleshooting. This includes node management, configuration control, performance monitoring, and diagnostic tools for investigating cluster health and log ingestion issues.

## Tech Stack

- **Framework**: React 18 with TypeScript
- **Routing**: React Router v6
- **Styling**:
  - Tailwind CSS for utility-first styling
  - Shadcn UI (built on Radix UI) for accessible components
  - Tailwind Merge for dynamic class merging
  - Tailwind Animate for animations
- **State Management**: React Context + Custom Hooks
- **Data Visualization**: Recharts
- **Development**:
  - Vite for build tooling and development server
  - TypeScript for type safety
  - ESLint for code quality
  - PostCSS for CSS processing

## Project Structure

```
src/
├── components/           # React components
│   ├── ui/              # Shadcn UI components
│   │   ├── errors/      # Error handling components
│   │   └── breadcrumbs/ # Navigation breadcrumbs
│   ├── shared/          # Shared components used across pages
│   │   └── {pagename}/  # Page-specific components
│   ├── common/          # Truly reusable components
│   └── features/        # Complex feature modules
│       └── theme/       # Theme-related components and logic
├── pages/               # Page components and routes
├── layout/              # Layout components
├── hooks/               # Custom React hooks
├── contexts/            # React context providers
├── lib/                # Utility functions and constants
└── types/              # TypeScript type definitions
```

## Component Organization Guidelines

1. **Page-Specific Components**
   - Place in `components/{pagename}/`
   - Only used by a single page
   - Colocated with the page they belong to

2. **Shared Components**
   - Place in `components/shared/`
   - Used across multiple pages
   - Well-documented and maintainable

3. **Common Components**
   - Place in `components/common/`
   - Highly reusable, pure components
   - No business logic

4. **UI Components**
   - Place in `components/ui/`
   - Shadcn components
   - Do not modify directly

## Development Guidelines

1. **TypeScript**
   - Use TypeScript for all new code
   - Prefer interfaces over types
   - Avoid enums, use const maps instead

2. **Component Best Practices**
   - Use functional components
   - Keep components small and focused
   - Use composition over inheritance
   - Colocate related code

3. **State Management**
   - Use React Context for global state
   - Custom hooks for reusable logic
   - Local state for component-specific data

4. **Styling**
   - Use Tailwind CSS for styling
   - Avoid inline styles
   - Use CSS variables for theming
   - Follow responsive design principles

## Getting Started

1. Install dependencies:

   ```bash
   npm install
   ```

2. Start development server:

   ```bash
   npm run dev
   ```

3. Build for production:

   ```bash
   npm run build
   ```

4. Lint code:

   ```bash
   npm run lint
   ```

## Contributing

1. Follow the folder structure
2. Write clean, maintainable code
3. Add proper TypeScript types
4. Document complex logic
