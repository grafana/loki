import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { TenantFilterSelect } from './tenant-filter-select';
import '@testing-library/jest-dom';

describe('TenantFilterSelect - Radix UI Select component tests', () => {
  const mockOnChange = jest.fn();
  const mockTenants = ['tenant-a', 'tenant-b', 'tenant-c', 'tenant-123'];

  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('rendering', () => {
    it('renders with no selection (All Tenants)', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      expect(select).toBeInTheDocument();
      expect(screen.getByText('All Tenants')).toBeInTheDocument();
    });

    it('renders with a selected tenant', () => {
      render(
        <TenantFilterSelect 
          value="tenant-b" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      expect(screen.getByText('tenant-b')).toBeInTheDocument();
    });

    it('renders all tenant options when opened', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      act(() => {
        fireEvent.click(select);
      });
      
      // Should show All Tenants option plus all provided tenants
      // Use getAllByText since 'All Tenants' appears in trigger and dropdown
      expect(screen.getAllByText('All Tenants').length).toBeGreaterThan(0);
      mockTenants.forEach(tenant => {
        expect(screen.getAllByText(tenant).length).toBeGreaterThan(0);
      });
    });

    it('renders with empty tenant list', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={[]} 
        />
      );
      
      expect(screen.getByRole('combobox')).toBeInTheDocument();
      expect(screen.getByText('All Tenants')).toBeInTheDocument();
    });
  });

  describe('selection', () => {
    it('selects a tenant when clicked', async () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      act(() => {
        fireEvent.click(select);
      });
      
      const tenantOption = screen.getAllByText('tenant-b')[0];
      act(() => {
        fireEvent.click(tenantOption);
      });
      
      expect(mockOnChange).toHaveBeenCalledWith('tenant-b');
    });

    it('selects All Tenants (undefined) when clicked', async () => {
      render(
        <TenantFilterSelect 
          value="tenant-a" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      act(() => {
        fireEvent.click(select);
      });
      
      const allOption = screen.getByText('All Tenants');
      act(() => {
        fireEvent.click(allOption);
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(undefined);
    });

    it('maintains selection after closing and reopening', async () => {
      render(
        <TenantFilterSelect 
          value="tenant-c" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      // Use getAllByText since it appears in trigger
      expect(screen.getAllByText('tenant-c').length).toBeGreaterThan(0);
      
      // Open and close dropdown
      const select = screen.getByRole('combobox');
      act(() => {
        fireEvent.click(select);
      });
      act(() => {
        fireEvent.click(document.body);
      }); // Click outside to close
      
      // Should still show selected value
      expect(screen.getAllByText('tenant-c').length).toBeGreaterThan(0);
    });
  });

  describe('clear functionality', () => {
    it('shows clear button when a tenant is selected', () => {
      render(
        <TenantFilterSelect 
          value="tenant-a" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear selection/i });
      expect(clearButton).toBeInTheDocument();
    });

    it('does not show clear button when no tenant is selected', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      expect(screen.queryByRole('button', { name: /clear/i })).not.toBeInTheDocument();
    });

    it('clears selection when clear button is clicked', () => {
      render(
        <TenantFilterSelect 
          value="tenant-b" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear selection/i });
      act(() => {
        fireEvent.click(clearButton);
      });
      
      expect(mockOnChange).toHaveBeenCalledWith(undefined);
    });

    it('prevents dropdown from opening when clicking clear button', () => {
      render(
        <TenantFilterSelect 
          value="tenant-a" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const clearButton = screen.getByRole('button', { name: /clear selection/i });
      act(() => {
        fireEvent.click(clearButton);
      });
      
      // Verify the selection was cleared and dropdown is closed
      expect(mockOnChange).toHaveBeenCalledWith(undefined);
      const select = screen.getByRole('combobox');
      expect(select).toHaveAttribute('aria-expanded', 'false');
    });
  });

  describe('sorting', () => {
    it('sorts tenant options alphabetically', () => {
      const unsortedTenants = ['zebra', 'alpha', 'charlie', 'bravo'];
      
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={unsortedTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      act(() => {
        fireEvent.click(select);
      });
      
      const options = screen.getAllByRole('option');
      // First option should be "All Tenants", then sorted tenants
      expect(options[0]).toHaveTextContent('All Tenants');
      expect(options[1]).toHaveTextContent('alpha');
      expect(options[2]).toHaveTextContent('bravo');
      expect(options[3]).toHaveTextContent('charlie');
      expect(options[4]).toHaveTextContent('zebra');
    });
  });

  describe('search/filter', () => {
    it('filters tenants when typing', async () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      // Type to filter - CommandInput has role="combobox"
      const searchInput = screen.getByPlaceholderText('Search tenants...');
      fireEvent.change(searchInput, { target: { value: 'tenant-1' } });
      
      await waitFor(() => {
        // Should only show matching tenant
        expect(screen.getByText('tenant-123')).toBeInTheDocument();
        expect(screen.queryByText('tenant-a')).not.toBeInTheDocument();
        expect(screen.queryByText('tenant-b')).not.toBeInTheDocument();
      });
    });

    it('shows no results message when filter matches nothing', async () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      fireEvent.click(select);
      
      const searchInput = screen.getByPlaceholderText('Search tenants...');
      fireEvent.change(searchInput, { target: { value: 'xyz-no-match' } });
      
      await waitFor(() => {
        expect(screen.getByText(/No tenants found/i)).toBeInTheDocument();
      });
    });
  });

  describe('edge cases', () => {
    it('handles null onChange gracefully', () => {
      expect(() => {
        render(
          <TenantFilterSelect 
            value={undefined} 
            onChange={null as unknown as () => void} 
            tenants={mockTenants} 
          />
        );
      }).not.toThrow();
    });

    it('handles duplicate tenants', () => {
      const duplicateTenants = ['tenant-a', 'tenant-a', 'tenant-b', 'tenant-b'];
      
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={duplicateTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      act(() => {
        fireEvent.click(select);
      });
      
      // Should deduplicate tenants
      const options = screen.getAllByRole('option');
      // All Tenants + 2 unique tenants
      expect(options).toHaveLength(3);
    });

    it('handles special characters in tenant names', () => {
      const specialTenants = ['tenant-@#$', 'tenant_123', 'tenant.com', 'tenant/slash'];
      
      render(
        <TenantFilterSelect 
          value="tenant-@#$" 
          onChange={mockOnChange} 
          tenants={specialTenants} 
        />
      );
      
      expect(screen.getByText('tenant-@#$')).toBeInTheDocument();
      
      const select = screen.getByRole('combobox');
      act(() => {
        fireEvent.click(select);
      });
      
      specialTenants.forEach(tenant => {
        expect(screen.getAllByText(tenant).length).toBeGreaterThan(0);
      });
    });

    it('handles very long tenant names', () => {
      const longTenant = 'a'.repeat(100);
      
      render(
        <TenantFilterSelect 
          value={longTenant} 
          onChange={mockOnChange} 
          tenants={[longTenant]} 
        />
      );
      
      // Should display without crashing
      expect(screen.getByText(longTenant)).toBeInTheDocument();
    });
  });

  describe('accessibility', () => {
    it('has proper ARIA labels', () => {
      render(
        <TenantFilterSelect 
          value={undefined} 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      const select = screen.getByRole('combobox');
      expect(select).toHaveAttribute('aria-expanded', 'false');
      
      act(() => {
        fireEvent.click(select);
      });
      
      expect(select).toHaveAttribute('aria-expanded', 'true');
    });

    it('supports screen reader announcements', () => {
      render(
        <TenantFilterSelect 
          value="tenant-a" 
          onChange={mockOnChange} 
          tenants={mockTenants} 
        />
      );
      
      // Selected value should be announced
      expect(screen.getByText('tenant-a')).toBeInTheDocument();
    });
  });
});
