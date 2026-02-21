'use client';

import { useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import * as d3 from 'd3';

const nodeColors: Record<string, string> = {
  decision: '#22c55e',
  session: '#3b82f6',
  commit: '#f59e0b',
  service: '#a855f7',
  policy: '#ef4444',
};

export default function LineagePage() {
  const params = useParams();
  const decisionId = params.decisionId as string;
  const svgRef = useRef<SVGSVGElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const { data: lineage, isLoading, error } = useQuery({
    queryKey: ['lineage', decisionId],
    queryFn: () => api.getDecisionLineage(decisionId, 5),
    enabled: !!decisionId,
  });

  useEffect(() => {
    if (!lineage || !svgRef.current || !containerRef.current) return;

    const svg = d3.select(svgRef.current);
    const container = containerRef.current;
    const width = container.clientWidth;
    const height = 500;

    svg.selectAll('*').remove();
    svg.attr('width', width).attr('height', height);

    // Arrow marker
    svg.append('defs').append('marker')
      .attr('id', 'arrowhead')
      .attr('viewBox', '-0 -5 10 10')
      .attr('refX', 20)
      .attr('refY', 0)
      .attr('orient', 'auto')
      .attr('markerWidth', 6)
      .attr('markerHeight', 6)
      .append('path')
      .attr('d', 'M 0,-5 L 10,0 L 0,5')
      .attr('fill', '#666');

    const g = svg.append('g');

    // Zoom
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.1, 4])
      .on('zoom', (event) => g.attr('transform', event.transform));
    svg.call(zoom);

    // Simulation
    const simulation = d3.forceSimulation(lineage.nodes as any)
      .force('link', d3.forceLink(lineage.edges as any).id((d: any) => d.id).distance(100))
      .force('charge', d3.forceManyBody().strength(-300))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(40));

    // Links
    const link = g.append('g')
      .selectAll('line')
      .data(lineage.edges)
      .enter()
      .append('line')
      .attr('stroke', '#666')
      .attr('stroke-opacity', 0.6)
      .attr('stroke-width', 2)
      .attr('marker-end', 'url(#arrowhead)');

    // Link labels
    const linkLabels = g.append('g')
      .selectAll('text')
      .data(lineage.edges)
      .enter()
      .append('text')
      .attr('font-size', 10)
      .attr('fill', '#888')
      .text(d => d.relationship);

    // Nodes
    const node = g.append('g')
      .selectAll('g')
      .data(lineage.nodes)
      .enter()
      .append('g')
      .call(d3.drag<SVGGElement, any>()
        .on('start', (event, d) => {
          if (!event.active) simulation.alphaTarget(0.3).restart();
          d.fx = d.x;
          d.fy = d.y;
        })
        .on('drag', (event, d) => {
          d.fx = event.x;
          d.fy = event.y;
        })
        .on('end', (event, d) => {
          if (!event.active) simulation.alphaTarget(0);
          d.fx = null;
          d.fy = null;
        }));

    node.append('circle')
      .attr('r', 15)
      .attr('fill', d => nodeColors[d.type] || '#666')
      .attr('stroke', '#fff')
      .attr('stroke-width', 2);

    node.append('text')
      .attr('dy', 30)
      .attr('text-anchor', 'middle')
      .attr('font-size', 11)
      .attr('fill', 'currentColor')
      .text(d => d.label.length > 20 ? d.label.substring(0, 17) + '...' : d.label);

    node.append('title')
      .text(d => `${d.type}: ${d.label}\n${d.timestamp ? new Date(d.timestamp).toLocaleString() : ''}`);

    simulation.on('tick', () => {
      link
        .attr('x1', (d: any) => d.source.x)
        .attr('y1', (d: any) => d.source.y)
        .attr('x2', (d: any) => d.target.x)
        .attr('y2', (d: any) => d.target.y);

      linkLabels
        .attr('x', (d: any) => (d.source.x + d.target.x) / 2)
        .attr('y', (d: any) => (d.source.y + d.target.y) / 2);

      node.attr('transform', (d: any) => `translate(${d.x},${d.y})`);
    });

    return () => {
      simulation.stop();
    };
  }, [lineage]);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-destructive p-4">
        Failed to load lineage: {error instanceof Error ? error.message : 'Unknown error'}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Decision Lineage</h1>
        <p className="text-muted-foreground">
          Visualize the decision chain and relationships
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            <span>Lineage Graph</span>
            <div className="flex gap-2">
              {Object.entries(nodeColors).map(([type, color]) => (
                <div key={type} className="flex items-center gap-1">
                  <div
                    className="w-3 h-3 rounded-full"
                    style={{ backgroundColor: color }}
                  />
                  <span className="text-xs text-muted-foreground capitalize">{type}</span>
                </div>
              ))}
            </div>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div ref={containerRef} className="w-full h-[500px] border rounded-lg bg-muted/20">
            <svg ref={svgRef} className="w-full h-full" />
          </div>
          <p className="text-xs text-muted-foreground mt-2">
            Drag nodes to reposition. Scroll to zoom. Pan by dragging the background.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
